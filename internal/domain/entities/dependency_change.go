package entities

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// MaxDependenciesPerEntry is the most dependencies one changelog line names.
// A run that moved more writes several lines, so a release note stays readable
// instead of becoming one sentence that wraps across a screen.
const MaxDependenciesPerEntry = 5

// changedThePrefix opens every dependency statement. "changed" is the verb the
// Keep a Changelog "Changed" section and the matching chlog kind are written in.
const changedThePrefix = "changed the "

// dependencyItemPattern matches one "`name` from `old` to `new`" item at the
// start of the text. Names and versions never contain a backtick, which is what
// makes every item fully delimited and the list unambiguous to read back.
var dependencyItemPattern = regexp.MustCompile("^`([^`]+)` from `([^`]+)` to `([^`]+)`")

// pinPattern matches the rest of a version pin statement, after its subject.
var pinPattern = regexp.MustCompile("^from `([^`]+)` to `([^`]+)`$")

// legacyPipelinePattern matches the per-upgrade statement the pipeline updater
// wrote before this grammar existed ("changed the golang pipeline version from
// `1.22` to `1.24`", or "action:actions/checkout" in place of the language),
// so a statement already pending is merged instead of repeated.
var legacyPipelinePattern = regexp.MustCompile("^(\\S+) pipeline version from `([^`]+)` to `([^`]+)`$")

// legacyActionPrefix marks a GitHub Action in a legacy pipeline statement.
const legacyActionPrefix = "action:"

// DependencyChange is one dependency a run moved, as the changelog states it.
type DependencyChange struct {
	Subject DependencySubject
	// Name identifies the dependency within its subject; it is empty for a
	// version pin.
	Name string
	From string
	To   string
}

// Key identifies the dependency a change is about, so that two statements about
// the same dependency are recognised as such whatever versions they name.
func (c DependencyChange) Key() string {
	if c.Subject.IsPin() {
		return string(c.Subject)
	}
	return string(c.Subject) + "\x00" + c.Subject.key(c.Name, c.To)
}

// RenderDependencyEntries renders changes as the Keep a Changelog bullets of new
// lines: version pins first, then each subject in registry order, names sorted
// case-insensitively, at most [MaxDependenciesPerEntry] dependencies per line.
func RenderDependencyEntries(changes []DependencyChange) []string {
	sorted := slices.Clone(changes)
	slices.SortStableFunc(sorted, compareForRendering)

	var entries []string
	for start := 0; start < len(sorted); {
		end := start + 1
		for end < len(sorted) && end-start < MaxDependenciesPerEntry &&
			sorted[end].Subject == sorted[start].Subject && !sorted[start].Subject.IsPin() {
			end++
		}
		entries = append(entries, "- "+DependencySentence(sorted[start:end]))
		start = end
	}
	return entries
}

// DependencySentence renders one statement, without a bullet marker, about
// changes that share a subject, keeping the order they are given in. A version
// pin is a single statement, so only its first change is rendered.
func DependencySentence(changes []DependencyChange) string {
	if len(changes) == 0 {
		return ""
	}

	subject := changes[0].Subject
	if subject.IsPin() {
		return fmt.Sprintf("%s%s from `%s` to `%s`", changedThePrefix, subject, changes[0].From, changes[0].To)
	}

	items := make([]string, 0, len(changes))
	for _, change := range changes {
		items = append(items, fmt.Sprintf("`%s` from `%s` to `%s`", change.Name, change.From, change.To))
	}
	return changedThePrefix + subject.phrase(len(changes)) + " " + joinItems(items)
}

// ParseDependencyEntry reads back a statement shaped the way [DependencySentence]
// writes one, returning the changes it names.
//
// Reading is lenient where people reformat by hand -- an optional bullet marker,
// a capital "Changed", a trailing period, an Oxford comma, a plural subject
// naming one item -- and strict everywhere else: the subject must be one the
// registry knows and every item must be fully delimited, so prose that only
// happens to mention a version is never read as a statement to rewrite. A
// statement spanning several lines is several statements, and is not read.
func ParseDependencyEntry(text string) ([]DependencyChange, bool) {
	body, ok := dependencyStatementBody(text)
	if !ok {
		return nil, false
	}

	for _, definition := range dependencySubjects {
		if changes, parsed := parseWithSubject(body, definition); parsed {
			return changes, true
		}
	}
	return parseLegacyPipelineStatement(body)
}

// dependencyStatementBody strips what precedes and follows the part of a
// statement that names a subject: the bullet marker, the leading "changed the"
// and a trailing period. Whitespace runs are folded, since a wrapped bullet
// reaches here joined by single spaces.
func dependencyStatementBody(text string) (string, bool) {
	if strings.ContainsAny(strings.TrimSpace(text), "\n\r") {
		return "", false
	}

	statement := strings.Join(strings.Fields(text), " ")
	for _, marker := range []string{"- ", "* "} {
		if after, found := strings.CutPrefix(statement, marker); found {
			statement = after
			break
		}
	}
	statement = strings.TrimSuffix(statement, ".")

	return cutPrefixFold(statement, changedThePrefix)
}

// parseWithSubject reads body as a statement about the given subject.
func parseWithSubject(body string, definition subjectDefinition) ([]DependencyChange, bool) {
	phrases := []string{string(definition.subject)}
	if definition.plural != "" {
		phrases = append([]string{definition.plural}, phrases...)
	}

	for _, phrase := range phrases {
		rest, found := cutPrefixFold(body, phrase+" ")
		if !found {
			continue
		}
		if definition.plural == "" {
			return parsePinStatement(definition.subject, rest)
		}
		return parseItemList(definition.subject, rest)
	}
	return nil, false
}

// parsePinStatement reads the "from `old` to `new`" that follows a pin subject.
func parsePinStatement(subject DependencySubject, rest string) ([]DependencyChange, bool) {
	match := pinPattern.FindStringSubmatch(rest)
	if match == nil {
		return nil, false
	}
	return []DependencyChange{{Subject: subject, From: match[1], To: match[2]}}, true
}

// parseItemList reads the comma- and "and"-separated items that follow a named
// subject.
func parseItemList(subject DependencySubject, rest string) ([]DependencyChange, bool) {
	var changes []DependencyChange
	for {
		match := dependencyItemPattern.FindStringSubmatch(rest)
		if match == nil {
			return nil, false
		}
		changes = append(changes, DependencyChange{
			Subject: subject, Name: match[1], From: match[2], To: match[3],
		})

		rest = rest[len(match[0]):]
		if rest == "" {
			return changes, true
		}

		separated := false
		for _, separator := range []string{", and ", ", ", " and "} {
			if after, found := strings.CutPrefix(rest, separator); found {
				rest, separated = after, true
				break
			}
		}
		if !separated {
			return nil, false
		}
	}
}

// parseLegacyPipelineStatement reads the statement the pipeline updater wrote
// per upgrade before this grammar existed.
func parseLegacyPipelineStatement(body string) ([]DependencyChange, bool) {
	match := legacyPipelinePattern.FindStringSubmatch(body)
	if match == nil {
		return nil, false
	}

	change := DependencyChange{Subject: SubjectPipelineRuntime, Name: match[1], From: match[2], To: match[3]}
	if action, found := strings.CutPrefix(match[1], legacyActionPrefix); found {
		change.Subject, change.Name = SubjectGitHubAction, action
	}
	return []DependencyChange{change}, true
}

// compareForRendering orders changes the way new statements are written:
// subjects in registry order, names case-insensitively within a subject.
func compareForRendering(a, b DependencyChange) int {
	if a.Subject != b.Subject {
		return a.Subject.order() - b.Subject.order()
	}
	if byName := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); byName != 0 {
		return byName
	}
	return strings.Compare(a.Name+"\x00"+a.To, b.Name+"\x00"+b.To)
}

// joinItems joins items the way a sentence lists them: "a", "a and b", "a, b
// and c".
func joinItems(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// cutPrefixFold is [strings.CutPrefix] with the prefix matched case-insensitively.
func cutPrefixFold(text, prefix string) (string, bool) {
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return text, false
	}
	return text[len(prefix):], true
}
