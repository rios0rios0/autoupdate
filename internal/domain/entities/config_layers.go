package entities

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	configEntities "github.com/rios0rios0/gitforge/pkg/config/domain/entities"
)

// The four configuration layers, in the order they are applied. Each one overrides only
// the keys its document declares. These strings are the vocabulary the README, CLAUDE.md
// and every log line use, so a message an operator sees can be traced back to the
// document that produced it.
const (
	LayerBuiltInDefaults   = "built-in defaults"
	LayerPublishedDefaults = "published defaults"
	LayerOperatorConfig    = "operator configuration"
	LayerProjectConfig     = "project configuration"
)

// LayerScope selects a layer's decode target, and with it the set of keys the layer is
// able to express at all.
type LayerScope int

const (
	// ScopeOperator decodes into Settings and may set every key, including
	// credentials, providers, the project list and the bump branch prefix. Only the file
	// the operator named with -c, or the one found in their home directory, gets it.
	ScopeOperator LayerScope = iota

	// ScopeRestricted decodes into RestrictedConfig, which has no field for a credential,
	// for providers, for projects or for the branch prefix. AutoUpdate's own shipped
	// defaults, the copy fetched from DefaultConfigURL and the .autoupdate.yaml inside the
	// repository being released all use it: none of the three is the operator speaking,
	// so none of them needs to be able to name a token or aim a branch deletion.
	ScopeRestricted
)

// ConfigLayer is one configuration source, already read into memory.
//
// Reading is deliberately somebody else's job. A layer is bytes, so the whole folding
// engine is testable without a file, a home directory or a network.
type ConfigLayer struct {
	// Name is the layer's place in the chain, one of the Layer* constants above.
	Name string

	// Origin is the path or URL the bytes came from, for logs and errors. It is empty
	// for the built-in defaults, which have no location an operator could look at.
	Origin string

	Data  []byte
	Scope LayerScope

	// Strict makes a key the schema does not know an error rather than a silently
	// ignored line. True for the documents this repository owns and for the operator's
	// own file, where a typo is a mistake worth hearing about; false for anything
	// fetched, which a newer release may have widened.
	Strict bool

	// Optional downgrades a decode failure to a warning and skips the layer, so a
	// document nobody wrote -- or a remote nobody can reach -- cannot stop a run.
	Optional bool
}

// describe names a layer the way a log line should: the layer's role, and where it came
// from when that is somewhere an operator can go and look.
func (l ConfigLayer) describe() string {
	if l.Origin == "" {
		return l.Name
	}
	return fmt.Sprintf("%s (%s)", l.Name, l.Origin)
}

// ResolveSettings folds the layers in order and finalises the result.
//
// Finalisation -- reading a token out of a file, expanding "~", resolving provider
// tokens, applying the environment fallbacks -- runs once, over the finished
// configuration. It cannot live inside ApplyLayer: a token path set by one layer and
// overridden by the next would otherwise be read from disk on the way past.
func ResolveSettings(layers []ConfigLayer) (*Settings, error) {
	config := &Settings{} //nolint:exhaustruct // every field is filled by the layers

	for _, layer := range layers {
		next, err := ApplyLayer(config, layer)
		if err != nil {
			if !layer.Optional {
				return nil, err
			}
			logger.Warnf("Ignoring the %s: %v", layer.describe(), err)
			continue
		}
		config = next
	}

	FinalizeSettings(config)

	return config, nil
}

// ApplyLayer folds one layer onto config and returns the result. config is not mutated.
//
// It is exported because the layers do not all arrive at the same time: the three
// operator-facing layers resolve before any repository is touched, while a project's own
// file only exists once the repository has been cloned.
func ApplyLayer(config *Settings, layer ConfigLayer) (*Settings, error) {
	if layer.Scope == ScopeRestricted {
		return applyRestrictedLayer(config, layer)
	}

	return applyOperatorLayer(config, layer)
}

// applyOperatorLayer decodes a layer straight onto the accumulated configuration.
//
// Decoding a document into a *non-zero* struct sets only the keys the document carries
// and leaves every other field exactly as the layers before it left it -- which is the
// layering, for free, for every scalar, pointer and slice, and is why no field has to
// become a pointer for an explicit "false" to stay distinguishable from an omission.
//
// A map is the one exception. yaml.v3 decodes each map value into a fresh zero element,
// so a language this layer names would *replace* the inherited one rather than merge
// with it. Emptying the field before the decode and merging afterwards is what keeps
// MergeUpdatersConfig's rules -- version files matched by path, extensions appended and
// de-duplicated -- in force across layers rather than only within one.
func applyOperatorLayer(config *Settings, layer ConfigLayer) (*Settings, error) {
	// A shallow copy is a real copy here: yaml.v3 replaces a slice and a map wholesale
	// rather than writing through them, so nothing the decode does can reach the
	// configuration this layer is being folded onto. That is what lets an optional layer
	// fail halfway without leaving a half-applied configuration behind.
	next := *config

	inherited := config.Updaters
	next.Updaters = nil

	decoder := yaml.NewDecoder(bytes.NewReader(layer.Data))
	decoder.KnownFields(layer.Strict)

	// A document that is nothing but comments -- which every commented example is, and
	// which the shipped defaults nearly are -- reports io.EOF rather than decoding
	// nothing. That is a layer with nothing to say, not a broken one.
	if err := decoder.Decode(&next); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to decode the %s: %w", layer.describe(), err)
	}

	next.Updaters = MergeUpdatersConfig(inherited, next.Updaters)

	return &next, nil
}

// applyRestrictedLayer decodes a layer through the narrower schema, so the keys it is not
// allowed to set have nowhere to land.
func applyRestrictedLayer(config *Settings, layer ConfigLayer) (*Settings, error) {
	restricted, err := decodeRestricted(layer)
	if err != nil {
		return nil, err
	}

	return restricted.applyTo(config, layer.describe()), nil
}

// decodeRestricted reads a layer through RestrictedConfig and reports the operator-only
// keys it tried to set.
func decodeRestricted(layer ConfigLayer) (RestrictedConfig, error) {
	var restricted RestrictedConfig //nolint:exhaustruct // the document decides what is set

	// Never strict. The point of RestrictedConfig is that it has no field for the keys a
	// restricted layer may not set, so strict decoding would turn "AutoUpdate ignored your
	// providers block" into "AutoUpdate refused to run".
	decoder := yaml.NewDecoder(bytes.NewReader(layer.Data))
	decoder.KnownFields(false)

	if err := decoder.Decode(&restricted); err != nil && !errors.Is(err, io.EOF) {
		return restricted, fmt.Errorf("failed to decode the %s: %w", layer.describe(), err)
	}

	warnOperatorOnlyKeys(layer)

	return restricted, nil
}

// ApplyRepoOverlay returns the settings one repository is processed with: the operator's
// settings with that repository's own project layer folded on top.
//
// The base is never mutated, and that is not tidiness. In `run` mode one *Settings is
// shared by every goroutine in the per-organization fan-out, so a layer that wrote through
// it would leak one repository's configuration into another's -- nondeterministically,
// which is the worst shape that bug can take.
func ApplyRepoOverlay(base *Settings, config *RepoConfig) (*Settings, error) {
	// base is nil when local mode could not load a configuration at all, which is a state
	// LocalController deliberately keeps working. There is nothing for the layer to override,
	// and applyTo would dereference it.
	if base == nil || config == nil || len(config.Layer) == 0 {
		return base, nil
	}

	//nolint:exhaustruct // Strict is false for a restricted layer by construction
	return ApplyLayer(base, ConfigLayer{
		Name:     LayerProjectConfig,
		Origin:   RepoConfigFile,
		Data:     config.Layer,
		Scope:    ScopeRestricted,
		Optional: true,
	})
}

// RestrictedConfig is what a configuration layer that is not the operator's own may say.
//
// It is a separate struct rather than a filter over Settings because a struct with no
// field for a key is not a check that can be got round: a `providers:` block in a target
// repository's .autoupdate.yaml has nowhere to land, whatever a later reader of this file
// believes about how it is called.
//
// The booleans are pointers so that "absent" and "false" stay distinguishable, which is
// what lets a layer turn an inherited default *off* rather than only confirm it.
type RestrictedConfig struct {
	Updaters             map[string]UpdaterConfig `yaml:"updaters"`
	CleanupStaleBranches *bool                    `yaml:"cleanup_stale_branches"`
	ExcludeForks         *bool                    `yaml:"exclude_forks"`
	ExcludeArchived      *bool                    `yaml:"exclude_archived"`
	ExcludeRepos         []string                 `yaml:"exclude_repos"`
}

// applyTo folds the restricted layer onto settings, returning a copy.
func (r RestrictedConfig) applyTo(settings *Settings, layerName string) *Settings {
	next := *settings

	if acceptSwitchOff(r.CleanupStaleBranches, layerName, "cleanup_stale_branches") {
		next.CleanupStaleBranches = r.CleanupStaleBranches
	}
	if r.ExcludeForks != nil {
		next.ExcludeForks = *r.ExcludeForks
	}
	if r.ExcludeArchived != nil {
		next.ExcludeArchived = *r.ExcludeArchived
	}
	if r.ExcludeRepos != nil {
		next.ExcludeRepos = r.ExcludeRepos
	}

	next.Updaters = MergeUpdatersConfig(settings.Updaters, r.Updaters)

	return &next
}

// acceptSwitchOff reports whether a restricted layer's toggle may be honoured.
//
// Stale-branch cleanup deletes remote branches and closes their pull requests, so a layer
// that is not the operator's may turn it *off* and never on. Off is safe in a way on is not:
// it can only ever remove an action.
//
// The ordering is what makes this necessary rather than merely tidy. applySkipCleanupFlag is
// applied to the resolved settings in the controller, and this layer is folded per repository
// afterwards -- so honouring an enable here would let a target repository override the
// --skip-cleanup an operator reached for precisely to stop branches being deleted.
func acceptSwitchOff(value *bool, layerName, key string) bool {
	if value == nil {
		return false
	}
	if !*value {
		return true
	}

	logger.Warnf(
		"Ignoring %q: %s from the %s can only turn it off, not on -- it deletes remote "+
			"branches and closes their pull requests, and --skip-cleanup is applied before "+
			"this layer", key, key, layerName,
	)

	return false
}

const (
	reasonCredential = "credentials are the operator's to configure and are never read " +
		"from a repository or from the network"
	reasonDiscovery = "which repositories to scan is the operator's to configure"
	reasonDeletion  = "the branch prefix decides which branches stale-branch cleanup " +
		"deletes, so it is the operator's alone to set"
	reasonFanOut = "concurrency is an organization-level setting, decided before any " +
		"repository's own file has been read"
)

// operatorOnlyKeys names the top-level keys a restricted layer may write and AutoUpdate
// will never honour. RestrictedConfig has no field for any of them, so this table changes
// nothing about what is decoded; it exists so that a document which tried is answered out
// loud instead of the setting silently doing nothing.
//
//nolint:gochecknoglobals // read-only lookup table
var operatorOnlyKeys = map[string]string{
	"providers":                 reasonDiscovery,
	"aggregate_branch_prefix":   reasonDeletion,
	"concurrency":               reasonFanOut,
	"github_access_token":       reasonCredential,
	"gitlab_access_token":       reasonCredential,
	"azure_devops_access_token": reasonCredential,
	"gpg_key_path":              reasonCredential,
	"gpg_key_passphrase":        reasonCredential,
}

// warnOperatorOnlyKeys reports the operator-only keys a restricted layer tried to set.
func warnOperatorOnlyKeys(layer ConfigLayer) {
	for _, key := range topLevelKeys(layer.Data) {
		if reason, rejected := operatorOnlyKeys[key]; rejected {
			logger.Warnf(
				"Ignoring %q from the %s: %s", key, layer.describe(), reason,
			)
		}
	}
}

// topLevelKeys returns the document's top-level mapping keys, or nothing when the
// document is empty or is not a mapping.
func topLevelKeys(data []byte) []string {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil
	}

	root := documentMapping(&document)
	if root == nil {
		return nil
	}

	keys := make([]string, 0, len(root.Content)/mappingStride)
	for i := 0; i < len(root.Content); i += mappingStride {
		keys = append(keys, root.Content[i].Value)
	}

	return keys
}

// mappingStride is how far apart a YAML mapping's keys are in Node.Content, which
// interleaves keys and values.
const mappingStride = 2

// documentMapping unwraps a decoded document down to its root mapping, or nil when there
// is not one -- an empty file, or a document whose root is a scalar or a sequence.
func documentMapping(document *yaml.Node) *yaml.Node {
	node := document
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}

	return node
}

// FinalizeSettings applies the resolution steps that belong to the finished configuration
// rather than to any one layer: a token read out of the file it names or the environment
// variable it references, and the environment consulted where a value is still empty.
//
// It runs once, over the folded result. Running it per layer would read a token path off
// disk even when the next layer replaced it.
func FinalizeSettings(settings *Settings) {
	for i := range settings.Providers {
		settings.Providers[i].Token = settings.Providers[i].ResolveToken()
	}

	settings.GpgKeyPassphrase = configEntities.ResolveToken(settings.GpgKeyPassphrase)
	settings.GitHubAccessToken = configEntities.ResolveToken(settings.GitHubAccessToken)
	settings.GitLabAccessToken = configEntities.ResolveToken(settings.GitLabAccessToken)
	settings.AzureDevOpsAccessToken = configEntities.ResolveToken(settings.AzureDevOpsAccessToken)

	settings.GitLabCIJobToken = os.Getenv("CI_JOB_TOKEN")

	if settings.GpgKeyPassphrase == "" {
		settings.GpgKeyPassphrase = os.Getenv("GPG_PASSPHRASE")
	}
}
