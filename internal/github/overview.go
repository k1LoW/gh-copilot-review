package github

import (
	"regexp"
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
	URL                string
	State              string
	Body               string
	Assessment         string
	Summary            string
	NotReadyToApprove  bool
	SuppressedComments []SuppressedComment
}

// NeedsAttention reports whether the overview itself raises something to
// address, even when no unresolved inline review comment remains.
func (o *CopilotReviewOverview) NeedsAttention() bool {
	if o == nil {
		return false
	}
	return o.NotReadyToApprove || o.State == "CHANGES_REQUESTED" || len(o.SuppressedComments) > 0
}

var (
	headingRe           = regexp.MustCompile(`^#{1,6}[ \t]+(.+?)[ \t]*$`)
	suppressedHeadingRe = regexp.MustCompile(`(?i)^#{1,6}[ \t]+suppressed comments\b`)
	suppressedEntryRe   = regexp.MustCompile(`^\*\*(.+?):(\d+)\*\*[ \t]*$`)
	metadataRe          = regexp.MustCompile(`^[-*][ \t]+\*\*`)
)

// parseCopilotReviewOverview extracts the review assessment and the findings
// Copilot only reported in the overview. Copilot renders the assessment as the
// leading heading ("Not ready to approve" and friends) and lists findings it
// did not post inline under a "Suppressed comments" heading, so both are
// recovered from the Markdown body rather than from a dedicated API field.
func parseCopilotReviewOverview(body string) *CopilotReviewOverview {
	o := &CopilotReviewOverview{Body: body}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")

	// Only a leading heading is an assessment. Bodies in the older overview
	// format open with prose yet still carry headings further down, inside
	// <details> ("Files not reviewed"), which must not be mistaken for one.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			o.Assessment = m[1]
			o.Summary = firstParagraph(lines[i+1:])
		}
		break
	}
	o.NotReadyToApprove = strings.Contains(strings.ToLower(o.Assessment), "not ready to approve")
	o.SuppressedComments = parseSuppressedComments(lines)

	return o
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

func parseSuppressedComments(lines []string) []SuppressedComment {
	start := -1
	for i, line := range lines {
		if suppressedHeadingRe.MatchString(line) {
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
		// The section ends at the next heading, at the review metadata list, or
		// at the end of the enclosing <details> block.
		if headingRe.MatchString(trimmed) || metadataRe.MatchString(trimmed) || strings.HasPrefix(trimmed, "</details>") {
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
