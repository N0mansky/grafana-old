package notifiers

import (
	"encoding/json"
	"fmt"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
	"github.com/grafana/grafana/pkg/services/alerting/conditions"
)

const defaultLarkMsgType = "card"
const larkNotifierDescription = `Use https://open.larksuite.com/open-apis/bot/v2/hook/xxxxxxxxx for larksuite.
Use https://open.feishu.cn/open-apis/bot/v2/hook/xxxxxxxxx for feishu.`

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
				Description:  larkNotifierDescription,
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
	if url == "" {
		return nil, alerting.ValidationError{Reason: "Could not find url property in settings"}
	}
	msgType := model.Settings.Get("msgType").MustString(defaultLarkMsgType)

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
	message := evalContext.Rule.Message
	title := evalContext.GetNotificationTitle()
	// Customized code head ---
	statusFlg := "red"
	alertStatus := evalContext.GetStateModel().Text
	duid, _ := evalContext.GetDashboardUID()
	moduleName := duid.Slug
	desc := evalContext.Rule.Name
	env := lark.Environment
	indexName := ""
	queryCondition := evalContext.Rule.Conditions[0].(*conditions.QueryCondition)
	queryStr := queryCondition.Query.Model.Get("query")
	for _, x := range evalContext.Rule.AlertRuleTags {
		switch x.Key {
		case "index_name":
			indexName = x.Value
		}
	}
	if alertStatus == "OK" {
		statusFlg = "green"
	}
	lark.log.Debug(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s", statusFlg, alertStatus,
		moduleName, desc, env, indexName, queryStr))
	// Customized code head tail
	lark.log.Info("message: " + message)
	lark.log.Info("title: " + title)
	if message == "" {
		message = title
	}

	for i, match := range evalContext.EvalMatches {
		message += fmt.Sprintf("\n%2d. %s: %s", i+1, match.Metric, match.Value)
	}
	message += fmt.Sprintf("\n%s", messageURL)

	var bodyMsg map[string]interface{}
	if lark.MsgType == "text" {
		content := map[string]string{
			"text": message,
		}

		bodyMsg = map[string]interface{}{
			"msg_type": "text",
			"content":  content,
		}
	} else if lark.MsgType == "card" {
		title := desc
		content := message
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
					"template": statusFlg,
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
