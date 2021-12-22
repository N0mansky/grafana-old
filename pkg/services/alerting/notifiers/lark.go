package notifiers

import (
	"encoding/json"
	"fmt"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
	"github.com/grafana/grafana/pkg/services/alerting/conditions"
)

const defaultLarkMsgType = "card"

func init() {
	alerting.RegisterNotifier(&alerting.NotifierPlugin{
		Type:        "lark",
		Name:        "Lark/Feishu",
		Description: "Sends HTTP POST request to Lark",
		Heading:     "Lark/Feishu settings",
		Factory:     newLarkNotifier,
		Options: []alerting.NotifierOption{
			{
				Label:        "Url",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxxx",
				PropertyName: "url",
				Required:     true,
			},
			{
				Label:        "Kibana Url",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "https://elk.internal.pingxx.com/xxxx",
				PropertyName: "kibUrl",
				Required:     true,
			},
			{
				Label:        "Message Type",
				Element:      alerting.ElementTypeSelect,
				PropertyName: "msgType",
				Required:     true,
				SelectOptions: []alerting.SelectOption{
					{
						Value: "card",
						Label: "Card",
					},
					{
						Value: "text",
						Label: "Text",
					},
				},
			},
			{
				Label:        "Environment",
				Element:      alerting.ElementTypeSelect,
				PropertyName: "environment",
				Required:     true,
				SelectOptions: []alerting.SelectOption{
					{
						Value: "sit",
						Label: "SIT",
					},
					{
						Value: "uat",
						Label: "UAT",
					},
					{
						Value: "prod",
						Label: "PROD",
					},
				},
			},
		},
	})
}

func newLarkNotifier(model *models.AlertNotification, _ alerting.GetDecryptedValueFn) (alerting.Notifier, error) {
	url := model.Settings.Get("url").MustString()
	env := model.Settings.Get("environment").MustString()
	kibUrl := model.Settings.Get("kibUrl").MustString()
	msgType := model.Settings.Get("msgType").MustString(defaultLarkMsgType)
	if url == "" {
		return nil, alerting.ValidationError{Reason: "Could not find url property in settings"}
	}

	return &LarkNotifier{
		NotifierBase: NewNotifierBase(model),
		MsgType:      msgType,
		URL:          url,
		KibUrl:       kibUrl,
		Environment:  env,
		log:          log.New("alerting.notifier.lark"),
	}, nil
}

// LarkNotifier is responsible for sending alert notifications to ding ding.
type LarkNotifier struct {
	NotifierBase
	MsgType     string
	Environment string
	KibUrl      string
	URL         string
	log         log.Logger
}

// Notify sends the alert notification to lark.
func (lark *LarkNotifier) Notify(evalContext *alerting.EvalContext) error {
	lark.log.Info("Sending lark")
	messageURL, err := evalContext.GetRuleURL()
	if err != nil {
		lark.log.Error("Failed to get messageUrl", "error", err, "lark", lark.Name)
		messageURL = ""
	}

	body, err := lark.genBody(evalContext, messageURL)
	if err != nil {
		return err
	}
	lark.log.Debug("body: " + string(body))
	lark.log.Debug("url: " + lark.URL)

	cmd := &models.SendWebhookSync{
		Url:  lark.URL,
		Body: string(body),
	}

	if err := bus.DispatchCtx(evalContext.Ctx, cmd); err != nil {
		lark.log.Error("Failed to send Lark", "error", err, "lark", lark.Name)
		return err
	}

	return nil
}

func (lark *LarkNotifier) genBody(evalContext *alerting.EvalContext, messageURL string) ([]byte, error) {
	lark.log.Info("messageUrl:" + messageURL)
	queryCondition := evalContext.Rule.Conditions[0].(*conditions.QueryCondition)
	queryStr := queryCondition.Query.Model.Get("query").MustString()
	rst := map[string]string{
		"rule_msg":     evalContext.Rule.Message,
		"title":        evalContext.GetNotificationTitle(),
		"alert_status": evalContext.GetStateModel().Text,
		"desc":         evalContext.Rule.Name,
		"env":          lark.Environment,
		"raw_query":    queryStr,
		"alert_color":  "red",
	}
	if rst["alert_status"] == "OK" {
		rst["alert_color"] = "green"
	}
	//  Parse tags and put them to rst
	for _, x := range evalContext.Rule.AlertRuleTags {
		rst[x.Key] = x.Value
	}
	// Parse logs and put them to rst
	logMap, _ := lark.parseLog(evalContext)
	for k, v := range logMap {
		rst[k] = v
	}
	lark.log.Info(fmt.Sprintf("rst map: %v", rst))
	for i, match := range evalContext.EvalMatches {
		rst["rule_msg"] += fmt.Sprintf("\n%2d. %s: %s", i+1, match.Metric, match.Value)
	}
	rst["rule_msg"] += fmt.Sprintf("\n%s", messageURL)
	var bodyMsg map[string]interface{}
	if lark.MsgType == "text" {
		content := map[string]string{
			"text": rst["rule_msg"],
		}

		bodyMsg = map[string]interface{}{
			"msg_type": "text",
			"content":  content,
		}
	} else if lark.MsgType == "card" {
		title := rst["desc"]
		content := rst["rule_message"]
		bodyMsg = map[string]interface{}{
			"msg_type": "interactive",
			"card": map[string]interface{}{
				"config": map[string]interface{}{
					"wide_screen_mode": true,
				},
				"header": map[string]interface{}{
					"title": map[string]interface{}{
						"tag":     "plain_text",
						"content": title,
					},
					"template": rst["alert_color"],
				},
				"elements": []interface{}{
					map[string]interface{}{
						"tag":     "markdown",
						"content": content,
					},
				},
			},
		}
	}
	return json.Marshal(bodyMsg)
}

func (lark *LarkNotifier) parseLog(evalContext *alerting.EvalContext) (map[string]string, error) {
	rst := map[string]string{"trace_id": "", "request_id": "", "message": "", "request_method": ""}
	rstLogEntry := evalContext.Logs[1].Data.(*simplejson.Json)
	resDat := rstLogEntry.GetPath("resp_data")
	resByte, err := resDat.MarshalJSON()
	resJson, err := simplejson.NewJson(resByte)
	hits := resJson.GetPath("response", "data", "responses").
		GetIndex(0).
		GetPath("hits", "hits").
		GetIndex(0)
	_source := hits.Get("_source")
	// Get document field from _source
	for k, v := range rst {
		rst[k] = _source.Get(k).MustString(v)
	}
	// Get index name from hit
	_index := hits.Get("_index").MustString("")
	rst["index"] = _index
	return rst, err
}
