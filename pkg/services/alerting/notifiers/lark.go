package notifiers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
	"github.com/grafana/grafana/pkg/services/alerting/conditions"
	"net/url"
	"text/template"
	"time"
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
				Placeholder:  "https://elk.internal.pingxx.com",
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
	rst := make(map[string]string)
	if !evalContext.IsTestRun {
		queryCondition := evalContext.Rule.Conditions[0].(*conditions.QueryCondition)
		queryStr := queryCondition.Query.Model.Get("query").MustString()
		messageURL, err := evalContext.GetRuleURL()
		if err != nil {
			lark.log.Error("Failed to get messageUrl", "error", err, "lark", lark.Name)
			messageURL = ""
		}
		rst = map[string]string{
			"rule_msg":     evalContext.Rule.Message,
			"messageUrl":   messageURL,
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
		// Generate log url
		reqDataJson := evalContext.Logs[0].Data.(*simplejson.Json)
		from := time.UnixMilli(reqDataJson.GetPath("from").MustInt64()).
			Add(-8 * time.Hour).Format("2006-01-02T15:04:05.000Z")
		to := time.UnixMilli(reqDataJson.GetPath("to").MustInt64()).
			Add(-8 * time.Hour).Format("2006-01-02T15:04:05.000Z")
		logUrl := fmt.Sprintf(`%s/app/kibana#/discover?_g=(refreshInterval:(display:Off,pause:!f,value:0),`+
			`time:(from:'%s',mode:absolute,to:'%s'))`+
			`&_a=(columns:!(_source),index:%s,interval:auto,`+
			`query:(query_string:(analyze_wildcard:!t,query:'%s')),sort:!('@timestamp',desc))`,
			lark.KibUrl, from, to, rst["index_pattern_id"], rst["raw_query"])
		resUri, err := url.Parse(logUrl)
		rst["logUrl"] = resUri.String()
	}
	for _, match := range evalContext.EvalMatches {
		rst[match.Metric] = fmt.Sprintf("%s", match.Value)
	}
	rst["rule_msg"] += fmt.Sprintf("\n%s", messageURL)
	var bodyMsg map[string]interface{}
	switch lark.MsgType {
	case "text":
		bodyMsg = map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": rst["rule_msg"],
			},
		}
	case "card":
		title := rst["desc"]
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
						"content": lark.renderTmpl(rst, rst["alert_status"]),
					},
				},
			},
		}
	}
	return json.Marshal(bodyMsg)
}

//parseLog parse log from elasticsearch and return a map[string]string
func (lark *LarkNotifier) parseLog(evalContext *alerting.EvalContext) (map[string]string, error) {
	// Get request Condition[0]: Query
	rst := map[string]string{
		"trace_id":       "",
		"request_id":     "",
		"message":        "",
		"request_method": "",
		"module":         "",
		"request_uri":    "",
	}
	// Get request result : Condition[0]: Query Result
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
	if len(rst["message"]) > 100 {
		rst["message"] = rst["message"][:100]
	}
	return rst, err
}

func (lark *LarkNotifier) renderTmpl(val map[string]string, t string) string {
	txt := ""
	switch t {
	case "Alerting":
		txt = "**状态:** {{.alert_status}}\n" +
			"**模块名称:** {{.module}}\n" +
			"**环境:** {{.env}}\n" +
			"**查询index:** {{.index}}\n" +
			"**查询query:** {{.raw_query}}\n" +
			"**请求方法:** {{.request_method}}\n" +
			"**请求地址:** {{.request_uri}}\n" +
			"**报错信息:** {{.message}}\n" +
			"**RequestID:** {{.request_id}}\n" +
			"**TraceID:** {{.trace_id}}\n" +
			"**报错数量:** {{.Count}}\n" +
			"**图表:** [Grafana]({{.messageUrl}})\n" +
			"**日志:** [Kibana]({{.logUrl}})\n" +
			"<at id={{.responder}}></at>"
	case "OK":
		txt = "**状态:** {{.alert_status}}\n" +
			"**模块名称:** {{.module}}\n" +
			"**环境:** {{.env}}\n" +
			"**查询index:** {{.index}}\n" +
			"**查询query:** {{.raw_query}}\n" +
			"**图表:** [Grafana]({{.messageUrl}})\n" +
			"**日志:** [Kibana]({{.logUrl}})\n" +
			"<at id={{.responder}}></at>"
	default:
		txt = "testing"
	}
	var txtRstByte bytes.Buffer
	tmpl, err := template.New(t).Parse(txt)
	if err != nil {
		lark.log.Error(err.Error())
		return txt
	}
	if err := tmpl.Execute(&txtRstByte, val); err != nil {
		lark.log.Error(err.Error())
		return txt
	}
	return txtRstByte.String()
}
