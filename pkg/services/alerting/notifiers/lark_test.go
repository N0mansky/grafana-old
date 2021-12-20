package notifiers

import (
	"context"
	"github.com/grafana/grafana/pkg/services/alerting"
	"github.com/grafana/grafana/pkg/services/encryption/ossencryption"
	"github.com/grafana/grafana/pkg/services/validations"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/models"
)

func TestLarkNotifier(t *testing.T) {
	t.Run("empty settings should return error", func(t *testing.T) {
		json := `{ }`

		settingsJSON, _ := simplejson.NewJson([]byte(json))
		model := &models.AlertNotification{
			Name:     "lark_testing",
			Type:     "lark",
			Settings: settingsJSON,
		}
		_, err := newLarkNotifier(model, ossencryption.ProvideService().GetDecryptedValue)
		require.Error(t, err)
	})

	t.Run("settings should trigger incident", func(t *testing.T) {
		json := `{ "url": "https://www.google.com" }`

		settingsJSON, _ := simplejson.NewJson([]byte(json))
		model := &models.AlertNotification{
			Name:     "lark_testing",
			Type:     "lark",
			Settings: settingsJSON,
		}

		not, err := newLarkNotifier(model, ossencryption.ProvideService().GetDecryptedValue)
		notifier := not.(*LarkNotifier)

		require.Nil(t, err)
		require.Equal(t, "lark_testing", notifier.Name)
		require.Equal(t, "lark", notifier.Type)
		require.Equal(t, "https://www.google.com", notifier.URL)

		t.Run("genBody should not panic", func(t *testing.T) {
			evalContext := alerting.NewEvalContext(context.Background(),
				&alerting.Rule{
					State:   models.AlertStateAlerting,
					Message: `{host="localhost"}`,
				}, &validations.OSSPluginRequestValidator{})
			_, err = notifier.genBody(evalContext, "")
			require.Nil(t, err)
		})
	})
}
