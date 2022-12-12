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
	"regexp"
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
				Label:        "Proxy URL",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "http://xxxxxxxxx:35366",
				PropertyName: "proxyUrl",
				Required:     false,
			},
			{
				Label:        "Kibana Url",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "https://elk.internal.xxxx.com",
				PropertyName: "kibUrl",
				Required:     true,
			},
			{
				Label:        "CAT Url",
				Element:      alerting.ElementTypeInput,
				InputType:    alerting.InputTypeText,
				Placeholder:  "https://xxxx.com",
				PropertyName: "catUrl",
				Required:     false,
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
				Label:        "Elastic Version",
				Element:      alerting.ElementTypeSelect,
				PropertyName: "esVer",
				Required:     true,
				SelectOptions: []alerting.SelectOption{
					{
						Value: "5",
						Label: "5.x",
					},
					{
						Value: "7",
						Label: "7.7+",
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
					{
						Value: "pet",
						Label: "PET",
					},
				},
			},
			{
				Label:        "UI Color",
				Element:      alerting.ElementTypeSelect,
				PropertyName: "uiColor",
				Required:     false,
				SelectOptions: []alerting.SelectOption{
					{
						Value: "blue",
						Label: "blue",
					},
					{
						Value: "wathet",
						Label: "wathet",
					},
					{
						Value: "turquoise",
						Label: "turquoise",
					},
					{
						Value: "green",
						Label: "green",
					},
					{
						Value: "yellow",
						Label: "yellow",
					},
					{
						Value: "orange",
						Label: "orange",
					},
					{
						Value: "red",
						Label: "red",
					},
					{
						Value: "carmine",
						Label: "carmine",
					},
					{
						Value: "violet",
						Label: "violet",
					},
					{
						Value: "purple",
						Label: "purple",
					},
					{
						Value: "indigo",
						Label: "indigo",
					},
					{
						Value: "grey",
						Label: "grey",
					},
				},
			},
		},
	})
}

func newLarkNotifier(model *models.AlertNotification, _ alerting.GetDecryptedValueFn) (alerting.Notifier, error) {
	url := model.Settings.Get("url").MustString()
	proxyUrl := model.Settings.Get("proxyUrl").MustString()
	uiColor := model.Settings.Get("uiColor").MustString("red")
	env := model.Settings.Get("environment").MustString()
	kibUrl := model.Settings.Get("kibUrl").MustString()
	catUrl := model.Settings.Get("catUrl").MustString()
	esVer := model.Settings.Get("esVer").MustString("7")
	msgType := model.Settings.Get("msgType").MustString(defaultLarkMsgType)
	if url == "" {
		return nil, alerting.ValidationError{Reason: "Could not find url property in settings"}
	}

	return &LarkNotifier{
		NotifierBase: NewNotifierBase(model),
		MsgType:      msgType,
		URL:          url,
		KibUrl:       kibUrl,
		CatUrl:       catUrl,
		ProxyUrl:     proxyUrl,
		UIColor:      uiColor,
		EsVer:        esVer,
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
	UIColor     string
	CatUrl      string
	ProxyUrl    string
	EsVer       string
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
		Url:        lark.URL,
		HttpHeader: map[string]string{"proxyUrl": lark.ProxyUrl},
		Body:       string(body),
	}
	if err := bus.DispatchCtx(evalContext.Ctx, cmd); err != nil {
		lark.log.Error("Failed to send Lark", "error", err, "lark", lark.Name)
		return err
	}

	return nil
}

func (lark *LarkNotifier) genBody(evalContext *alerting.EvalContext, messageURL string) ([]byte, error) {
	lark.log.Info("messageUrl:" + messageURL)
	alertStatus := evalContext.GetStateModel().Text
	alertColor := lark.UIColor
	if alertStatus == "OK" {
		alertColor = "green"
	}
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
			"rule_msg":   evalContext.Rule.Message,
			"messageUrl": messageURL,
			"title":      evalContext.GetNotificationTitle(),
			"env":        lark.Environment,
			"raw_query":  queryStr,
		}
		// Parse Response logs and put them to rst
		logResMap, _ := lark.parseResLog(evalContext)
		for k, v := range logResMap {
			rst[k] = v
		}
		// Parse Request logs and put them to rst
		logReqMap := lark.parseReqLog(evalContext)
		for k, v := range logReqMap {
			rst[k] = v
		}
		//  Parse tags and put them to rst
		for _, x := range evalContext.Rule.AlertRuleTags {
			rst[x.Key] = x.Value
		}
	}
	for _, match := range evalContext.EvalMatches {
		rst[match.Metric] = fmt.Sprintf("%s", match.Value)
	}
	var bodyMsg map[string]interface{}
	switch lark.MsgType {
	case "text":
		bodyMsg = map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": rst["message"],
			},
		}
	case "card":
		title := rst["title"]
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
					"template": alertColor,
				},
				"elements": []interface{}{
					map[string]interface{}{
						"tag":     "markdown",
						"content": lark.renderTmpl(rst, evalContext),
					},
				},
			},
		}
	}
	return json.Marshal(bodyMsg)
}

//parseResLog parse log from elasticsearch and return a map[string]string
func (lark *LarkNotifier) parseResLog(evalContext *alerting.EvalContext) (map[string]string, error) {
	// Get request result : Condition[0]: Query Result
	dbRef, err := evalContext.GetDashboardUID()
	defaultModuleName := dbRef.Slug
	if err != nil {
		defaultModuleName = ""
	}
	rst := map[string]string{
		"trace_id":   "",
		"request_id": "",
		"message":    "",
		"cat_url":    "",
		"module":     defaultModuleName,
	}
	rstLogEntry := evalContext.Logs[1].Data.(*simplejson.Json)
	resDat := rstLogEntry.GetPath("resp_data")
	resByte, err := resDat.MarshalJSON()
	resJson, err := simplejson.NewJson(resByte)
	queryData := resJson.GetPath("request", "data").MustString("")
	r, _ := regexp.Compile(`index":"(.*)","`)
	rst["index"] = r.FindStringSubmatch(queryData)[1]
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
	msgRune := []rune(rst["message"])
	if len(msgRune) > 200 {
		rst["message"] = string(msgRune[:200])
	}
	return rst, err
}

func (lark *LarkNotifier) parseReqLog(evalContext *alerting.EvalContext) map[string]string {
	// Get request Condition[0]: Query
	reqDataJson := evalContext.Logs[0].Data.(*simplejson.Json)
	from := time.UnixMilli(reqDataJson.GetPath("from").MustInt64()).
		Add(-8 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	to := time.UnixMilli(reqDataJson.GetPath("to").MustInt64()).
		Add(-8 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	rst := map[string]string{
		"from": from,
		"to":   to,
	}
	return rst
}

func (lark *LarkNotifier) renderTmpl(val map[string]string, evalContext *alerting.EvalContext) string {
	txt := ""
	logUrl := ""
	loc, _ := time.LoadLocation("Asia/Shanghai")
	st := evalContext.Rule.LastStateChange.In(loc)
	val["stateTime"] = st.Format("2006-01-02 15:04:05")
	switch lark.EsVer {
	case "5":
		logUrl = fmt.Sprintf(`%s/app/kibana#/discover?_g=(refreshInterval:(display:Off,pause:!f,value:0),`+
			`time:(from:'%s',mode:absolute,to:'%s'))`+
			`&_a=(columns:!(_source),index:%s,interval:auto,`+
			`query:(query_string:(analyze_wildcard:!t,query:'%s')),sort:!('@timestamp',desc))`,
			lark.KibUrl, val["from"], val["to"], val["index_pattern_id"], val["raw_query"])
	case "7":
		logUrl = fmt.Sprintf(`%s/app/kibana#/discover?_g=(filters:!(),refreshInterval:(pause:!t,value:0),`+
			`time:(from:'%s',to:'%s'))`+
			`&_a=(columns:!(_source),filters:!(),index:'%s',interval:auto,`+
			`query:(language:lucene,query:' %s '),sort:!())`,
			lark.KibUrl, val["from"], val["to"], val["index_pattern_id"], val["raw_query"])
	}
	if lark.CatUrl != "" && val["cat_url"] != "" {
		cat_url := val["cat_url"]
		u, _ := url.Parse(cat_url)
		fmt.Printf(u.Path)
		val["cat_url"] = fmt.Sprintf(`%s%s?%v`, lark.CatUrl, u.Path, u.RawQuery)
	}
	resUri, err := url.Parse(logUrl)
	val["logUrl"] = resUri.String()
	alertStatus := evalContext.GetStateModel().Text
	switch alertStatus {
	case "Alerting":
		txt = "**模块名称:** {{.module}}\n" +
			"**环境:** {{.env}}\n" +
			"**发生时间:** {{.stateTime}}\n" +
			"**查询index:** {{.index}}\n" +
			"**查询query:** {{.raw_query}}\n" +
			"**RequestID:** {{.request_id}}\n" +
			//"**TraceID:** {{.trace_id}}\n" +
			"**报错数量:** {{.Count}}\n" +
			"**报错日志:** {{.message}}\n" +
			"**规则信息:** {{.rule_msg}}\n" +
			"**CAT:** [CAT]({{.cat_url}})\n" +
			"**图表:** [Grafana]({{.messageUrl}})\n" +
			"**日志:** [Kibana]({{.logUrl}})"
		//"**日志:** [Kibana]({{.logUrl}})\n"
		//	"<at id=all></at>"
	case "OK":
		txt = "**模块名称:** {{.module}}\n" +
			"**环境:** {{.env}}\n" +
			"**恢复时间:** {{.stateTime}}\n" +
			"**查询index:** {{.index}}\n" +
			"**查询query:** {{.raw_query}}\n" +
			"**规则信息:** {{.rule_msg}}\n" +
			"**图表:** [Grafana]({{.messageUrl}})\n" +
			"**日志:** [Kibana]({{.logUrl}})"
		//"**日志:** [Kibana]({{.logUrl}})\n" +
		//"<at id=all></at>"
	default:
		txt = "testing"
	}
	var txtRstByte bytes.Buffer
	tmpl, err := template.New(alertStatus).Parse(txt)
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
