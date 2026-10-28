package web

import "context"

var SettingsTabs = []string{"media", "metadata", "interface", "other"}

func SettingsTab(tab string) string {
	switch tab {
	case "media", "metadata", "interface", "other":
		return tab
	default:
		return "media"
	}
}

func SettingsURL(tab string) string { return "/settings?tab=" + SettingsTab(tab) }

func (d SettingsData) normalized() SettingsData {
	d.Tab = SettingsTab(d.Tab)
	d.Sources.Layout = d.Layout
	if d.Sources.Sources == nil {
		d.Sources.Sources = d.Settings.Redacted().Sources
	}
	return d
}

type mutationEpochKey struct{}

func WithMutationEpoch(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, mutationEpochKey{}, token)
}

func mutationEpoch(ctx context.Context) string {
	token, _ := ctx.Value(mutationEpochKey{}).(string)
	return token
}
