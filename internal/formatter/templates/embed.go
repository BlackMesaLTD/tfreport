// Package templates ships the default tfreport output templates. Each
// target has a single-report variant and (when relevant) a multi-report
// variant selected at render time by the arity of the BlockContext.
//
// The .tmpl files are intentionally small: heavy lifting is in blocks.
// Users can override any target with their own template via
// output.targets.<name>.template or .template_file in .tfreport.yml.
package templates

import _ "embed"

//go:embed markdown.tmpl
var markdown string

//go:embed markdown.multi.tmpl
var markdownMulti string

//go:embed github-pr-body.tmpl
var githubPRBody string

//go:embed github-pr-body.multi.tmpl
var githubPRBodyMulti string

//go:embed github-pr-comment.tmpl
var githubPRComment string

//go:embed github-pr-comment.multi.tmpl
var githubPRCommentMulti string

//go:embed github-step-summary.tmpl
var githubStepSummary string

//go:embed github-step-summary.multi.tmpl
var githubStepSummaryMulti string

// Default returns the embedded default template for the given target. If
// multi is true, the multi-report variant is returned when one exists;
// otherwise the single-report template is returned (it's callers'
// responsibility to supply compatible data).
func Default(target string, multi bool) string {
	if multi {
		switch target {
		case "markdown":
			return markdownMulti
		case "github-pr-body":
			return githubPRBodyMulti
		case "github-pr-comment":
			return githubPRCommentMulti
		case "github-step-summary":
			return githubStepSummaryMulti
		}
	}
	switch target {
	case "markdown":
		return markdown
	case "github-pr-body":
		return githubPRBody
	case "github-pr-comment":
		return githubPRComment
	case "github-step-summary":
		return githubStepSummary
	}
	return ""
}
