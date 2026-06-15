package web

// logLevelClass returns a CSS class for a log level label.
func logLevelClass(level string) string {
	switch level {
	case "ERROR":
		return "text-rose-400"
	case "WARN":
		return "text-amber-400"
	case "DEBUG":
		return "text-slate-500"
	default:
		return "text-emerald-400"
	}
}
