package github

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// SuppressedComment is a finding Copilot reported inside its review overview
// instead of posting it as an inline review comment.
type SuppressedComment struct {
	Path string
	Line int
	Body string
}

// CopilotReviewOverview is the parsed review overview (the review body) of a
// submitted Copilot review.
type CopilotReviewOverview struct {
	URL        string
	State      string
	Body       string
	Assessment string
	Summary    string
	// Approving reports whether Assessment explicitly approves the change.
	Approving          bool
	SuppressedComments []SuppressedComment
}

// NeedsAttention reports whether the overview itself raises something to
// address, even when no unresolved inline review comment remains.
// Any assessment that does not explicitly approve counts, because the exact
// wording Copilot uses to decline is not fixed.
func (o *CopilotReviewOverview) NeedsAttention() bool {
	if o == nil {
		return false
	}
	if o.State == "CHANGES_REQUESTED" || len(o.SuppressedComments) > 0 {
		return true
	}
	return o.Assessment != "" && !o.Approving
}

var (
	headingRe         = regexp.MustCompile(`^#{1,6}[ \t]+(.+?)[ \t]*$`)
	summaryRe         = regexp.MustCompile(`(?i)^<summary>(.*?)</summary>`)
	suppressedTitleRe = regexp.MustCompile(`(?i)^suppressed comments\b`)
	suppressedEntryRe = regexp.MustCompile(`^\*\*(.+?):(\d+)\*\*[ \t]*$`)
	metadataRe        = regexp.MustCompile(`^[-*][ \t]+\*\*`)
	// An assessment approves only when it opens with one of these phrases.
	// Matching declining wording instead would have to enumerate every way
	// Copilot can say no ("Cannot approve" hides "approve" behind no bare
	// "not"), and every phrase left out would read as an approval.
	approvingRe = regexp.MustCompile(`(?i)^(ready to approve|approved?|lgtm|looks good)\b`)
)

// structuralTitles are the section titles Copilot renders in a review overview.
// They can appear as the leading heading of a body that carries no assessment
// at all, so they must never be reported as one.
var structuralTitles = []string{
	"pull request overview",
	"file summaries",
	"review details",
	"files not reviewed",
	"suppressed comments",
	"comments suppressed",
}

// parseCopilotReviewOverview extracts the review assessment and the findings
// Copilot only reported in the overview. Copilot renders the assessment as the
// leading heading ("Not ready to approve" and friends) and lists findings it
// did not post inline under a "Suppressed comments" section, so both are
// recovered from the Markdown body rather than from a dedicated API field.
func parseCopilotReviewOverview(body string) *CopilotReviewOverview {
	o := &CopilotReviewOverview{Body: body}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	// Only a leading heading can be an assessment, and only when it is not one
	// of the section titles Copilot always renders: a body carrying no
	// assessment opens with "Pull request overview", and other bodies bury
	// section headings inside <details> further down.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil && !isStructuralTitle(m[1]) {
			o.Assessment = m[1]
			o.Summary = firstParagraph(lines[i+1:])
		}
		break
	}
	o.Approving = o.Assessment != "" && approvingRe.MatchString(trimTitle(o.Assessment))
	o.SuppressedComments = parseSuppressedComments(lines)

	return o
}

// isStructuralTitle reports whether a heading or <summary> text is one of
// Copilot's own section titles rather than prose it wrote for this review.
func isStructuralTitle(title string) bool {
	return slices.Contains(structuralTitles, strings.ToLower(trimTitle(title)))
}

// trimTitle strips the decoration around a title so it can be compared as
// words: assessments carry a leading status emoji, section titles a trailing
// count ("(3)").
func trimTitle(title string) string {
	return strings.TrimFunc(title, func(r rune) bool {
		return ('a' > r || r > 'z') && ('A' > r || r > 'Z')
	})
}

// firstParagraph returns the first prose paragraph, skipping the italic
// boilerplate Copilot appends after the assessment.
func firstParagraph(lines []string) string {
	var paragraph []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") {
			break
		}
		if len(paragraph) == 0 && strings.HasPrefix(trimmed, "*") && strings.HasSuffix(trimmed, "*") {
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	return strings.Join(paragraph, " ")
}

// isSuppressedSectionStart reports whether a line opens the suppressed
// comments section. Copilot titles it either as a Markdown heading or as the
// <summary> of its own <details> block, depending on the overview layout.
func isSuppressedSectionStart(line string) bool {
	if m := headingRe.FindStringSubmatch(line); m != nil {
		return suppressedTitleRe.MatchString(m[1])
	}
	if m := summaryRe.FindStringSubmatch(line); m != nil {
		return suppressedTitleRe.MatchString(strings.TrimSpace(m[1]))
	}
	return false
}

func parseSuppressedComments(lines []string) []SuppressedComment {
	start := -1
	for i, line := range lines {
		if isSuppressedSectionStart(strings.TrimSpace(line)) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}

	var comments []SuppressedComment
	var current *SuppressedComment
	var body []string
	inCode := false

	flush := func() {
		if current == nil {
			return
		}
		current.Body = strings.Join(body, "\n")
		comments = append(comments, *current)
		current = nil
		body = nil
	}

	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if m := suppressedEntryRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			n, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			current = &SuppressedComment{Path: m[1], Line: n}
			continue
		}
		// The section ends at the next heading or <details> block, at the review
		// metadata list, or at the end of the enclosing <details> block.
		if headingRe.MatchString(trimmed) || metadataRe.MatchString(trimmed) ||
			strings.HasPrefix(trimmed, "</details>") || strings.HasPrefix(trimmed, "<details") ||
			summaryRe.MatchString(trimmed) {
			break
		}
		if current == nil || trimmed == "" {
			continue
		}
		body = append(body, strings.TrimPrefix(strings.TrimPrefix(trimmed, "* "), "*"))
	}
	flush()

	return comments
}
