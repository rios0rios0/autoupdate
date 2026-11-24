package terraform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
)

// errNoRegistryClient is what a registry check reports when the updater was built without a
// way to ask a registry.
var errNoRegistryClient = errors.New("no registry client is configured")

// plainVersionPattern is the only shape an image pin is moved from or to: dotted numbers,
// with an optional leading `v`. A pre-release, a suffix such as `-alpine` or a word is a
// choice the file made, never a candidate.
var plainVersionPattern = regexp.MustCompile(`^[vV]?\d+(?:\.\d+)*$`)

// heldReason says why a newer tag was not applied to an image pin.
type heldReason int

const (
	// heldNotInRegistry: a registry the file pulls from does not hold the tag.
	heldNotInRegistry heldReason = iota
	// heldUnverified: a registry the file pulls from could not be asked.
	heldUnverified
	// heldVersionLine: the tag is on another version line than the pin.
	heldVersionLine
)

// heldUpgrade is a newer tag an image pin was not moved to.
type heldUpgrade struct {
	dep       entities.Dependency
	candidate string
	reason    heldReason
	registry  string
}

// describe says why the tag was not applied, for the log and the pull request.
func (h heldUpgrade) describe() string {
	switch h.reason {
	case heldNotInRegistry:
		return fmt.Sprintf("`%s` does not hold it", h.registry)
	case heldUnverified:
		return fmt.Sprintf("`%s` could not be asked whether it holds it", h.registry)
	case heldVersionLine:
		return fmt.Sprintf("it is on another version line than `%s`: review it and move the pin by hand",
			h.dep.CurrentVer)
	}
	return "it was not applied"
}

// registryListing is what one registry answered for one image.
type registryListing struct {
	tags map[string]struct{}
	err  error
}

// registryLookup identifies a listing in the planner's cache.
type registryLookup struct {
	registry   string
	repository string
}

// imagePlanner chooses the tag each image pin of one repository moves to.
//
// Nothing in a pull request builds or runs a pinned image -- a Terragrunt plan only passes
// the string along -- so the checks that make an unattended upgrade safe elsewhere never see
// it. Two things are therefore settled before a pin moves. When the operator names the
// registry the file pulls from (image_registries), the tag must be one that registry holds:
// the repository's tags say what was released, not what was published, and a pin naming an
// image that was never pushed fails only once a cluster tries to pull it. And the tag must
// continue the pin's version line, because a jump to another one is a rollout to plan, not a
// dependency bump, whatever allow_major_updates says.
type imagePlanner struct {
	client   repositories.ImageRegistryRepository
	rules    []entities.ImageRegistryRule
	repo     entities.Repository
	listings map[registryLookup]registryListing
}

// newImagePlanner returns a planner for repo's image pins.
func newImagePlanner(
	client repositories.ImageRegistryRepository,
	rules []entities.ImageRegistryRule,
	repo entities.Repository,
) *imagePlanner {
	return &imagePlanner{
		client:   client,
		rules:    rules,
		repo:     repo,
		listings: make(map[registryLookup]registryListing),
	}
}

// choose returns the tag dep moves to -- "" when it stays -- and the newer tags it was not
// moved to, at most one per reason, each the newest of its kind.
//
// The candidates are walked newest first. A tag a registry lacks is passed over for the next
// one, so a release whose image is missing does not hold back the release before it; a tag
// on another version line is passed over the same way, so the pin still moves along its own
// line. A registry that cannot be asked ends the walk: no older tag could be verified either.
func (p *imagePlanner) choose(ctx context.Context, dep entities.Dependency, released []string) (string, []heldUpgrade) {
	registries := entities.ImageRegistriesFor(p.rules, p.repo, dep.FilePath)

	var held []heldUpgrade
	reported := make(map[heldReason]bool)
	hold := func(candidate string, reason heldReason, registry string) {
		if reported[reason] {
			return
		}
		reported[reason] = true
		held = append(held, heldUpgrade{dep: dep, candidate: candidate, reason: reason, registry: registry})
	}

	for _, candidate := range imageCandidates(dep.CurrentVer, released) {
		verdict := p.verify(ctx, registries, dep.Source, candidate)
		if verdict.err != nil {
			hold(candidate, heldUnverified, verdict.registry)
			logger.Warnf("[terraform] %s/%s: could not ask %s for the tags of %s: %v",
				p.repo.Organization, p.repo.Name, verdict.registry, dep.Source, verdict.err)
			return "", held
		}
		if !verdict.holds {
			hold(candidate, heldNotInRegistry, verdict.registry)
			continue
		}
		if !sameVersionLine(dep.CurrentVer, candidate) {
			hold(candidate, heldVersionLine, "")
			continue
		}
		return candidate, held
	}

	return "", held
}

// registryVerdict is what the registries a file pulls from answered for one tag: whether all
// of them hold it, and otherwise the one that does not, or that could not be asked.
type registryVerdict struct {
	holds    bool
	registry string
	err      error
}

// verify asks every registry a file pulls from whether it holds image at tag.
func (p *imagePlanner) verify(
	ctx context.Context, registries []entities.ImageRegistryRule, image, tag string,
) registryVerdict {
	for _, rule := range registries {
		listing := p.listing(ctx, rule.Registry, rule.ImageRepository(image))
		if listing.err != nil {
			return registryVerdict{holds: false, registry: rule.Registry, err: listing.err}
		}
		if _, ok := listing.tags[tag]; !ok {
			return registryVerdict{holds: false, registry: rule.Registry, err: nil}
		}
	}
	return registryVerdict{holds: true, registry: "", err: nil}
}

// listing asks a registry for an image's tags, once per scan.
func (p *imagePlanner) listing(ctx context.Context, registry, repository string) registryListing {
	key := registryLookup{registry: strings.ToLower(registry), repository: repository}
	if cached, ok := p.listings[key]; ok {
		return cached
	}

	listing := registryListing{tags: nil, err: errNoRegistryClient}
	if p.client != nil {
		tags, err := p.client.ListTags(ctx, registry, repository)
		listing = registryListing{tags: make(map[string]struct{}, len(tags)), err: err}
		for _, tag := range tags {
			listing.tags[tag] = struct{}{}
		}
	}

	p.listings[key] = listing
	return listing
}

// imageCandidates returns the tags an image pin may move to, newest first: plain dotted
// versions strictly newer than the pin. When the pin is not a plain dotted version itself,
// nothing is.
//
// The tags are ordered here rather than trusted in the order they arrive: a provider sorts a
// list mixing `0.0.4` with `0.26.0.5` as strings once one of them is not semver, and the
// order is what decides which tag is offered first.
func imageCandidates(current string, tags []string) []string {
	if !plainVersionPattern.MatchString(current) {
		return nil
	}

	var candidates []string
	for _, tag := range tags {
		if plainVersionPattern.MatchString(tag) && support.IsNewerVersion(current, tag) {
			candidates = append(candidates, tag)
		}
	}

	slices.SortStableFunc(candidates, func(a, b string) int {
		switch {
		case support.IsNewerVersion(b, a):
			return -1
		case support.IsNewerVersion(a, b):
			return 1
		}
		return 0
	})
	return candidates
}

// sameVersionLine reports whether candidate continues the pin's version line: written with
// as many parts as the pin, and on the same major.
//
// A change in the number of parts is a change of scheme -- `0.0.4` to `0.26.0.5` is an image
// that started following an upstream release with a build number appended -- and a change
// of major is a change of line by definition. Both are rollouts somebody plans, not bumps.
func sameVersionLine(current, candidate string) bool {
	currentParts := strings.Split(strings.TrimLeft(current, "vV"), ".")
	candidateParts := strings.Split(strings.TrimLeft(candidate, "vV"), ".")
	if len(currentParts) != len(candidateParts) {
		return false
	}

	currentMajor, currentErr := strconv.Atoi(currentParts[0])
	candidateMajor, candidateErr := strconv.Atoi(candidateParts[0])
	return currentErr == nil && candidateErr == nil && currentMajor == candidateMajor
}

// logHeldUpgrades reports every newer tag an image pin was not moved to.
func logHeldUpgrades(repo entities.Repository, held []heldUpgrade) {
	for _, h := range held {
		logger.Warnf("[terraform] %s/%s: left %s at %s in %s although %s is newer: %s",
			repo.Organization, repo.Name, h.dep.Source, h.dep.CurrentVer, h.dep.FilePath,
			h.candidate, strings.ReplaceAll(h.describe(), "`", ""))
	}
}

// heldUpgradesSection renders the newer tags this pull request does not apply, or "" when
// there are none.
func heldUpgradesSection(held []heldUpgrade) string {
	if len(held) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n### Not applied\n\n")
	sb.WriteString("These image pins have a newer tag that this pull request leaves out:\n\n")
	sb.WriteString("| Image | File | In use | Newer tag | Why |\n")
	sb.WriteString("|-------|------|--------|-----------|-----|\n")
	for _, h := range held {
		fmt.Fprintf(&sb, "| `%s` | %s | `%s` | `%s` | %s |\n",
			h.dep.Source, h.dep.FilePath, h.dep.CurrentVer, h.candidate, h.describe())
	}
	return sb.String()
}
