package scanner

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// ModuleDependency represents a Terraform module dependency
type ModuleDependency struct {
	Name     string // Module name (label in terraform)
	Source   string // Source URL/path
	Version  string // Version tag (if using ?ref=)
	FilePath string // File where this dependency was found
	Line     int    // Line number in the file
}

// ScanTerraformFile parses a Terraform file and extracts module dependencies
func ScanTerraformFile(content, filePath string) ([]ModuleDependency, error) {
	parser := hclparse.NewParser()

	file, diags := parser.ParseHCL([]byte(content), filePath)
	if diags.HasErrors() {
		// Try regex-based parsing as fallback
		return scanWithRegex(content, filePath)
	}

	body := file.Body
	if body == nil {
		return nil, nil
	}

	var deps []ModuleDependency

	// Get all module blocks from the body
	bodyContent, _, diags := body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "module", LabelNames: []string{"name"}},
		},
	})
	if diags.HasErrors() {
		return scanWithRegex(content, filePath)
	}

	for _, block := range bodyContent.Blocks {
		if block.Type != "module" {
			continue
		}

		moduleName := ""
		if len(block.Labels) > 0 {
			moduleName = block.Labels[0]
		}

		// Extract source attribute
		attrs, _ := block.Body.JustAttributes()

		sourceAttr, hasSource := attrs["source"]
		if !hasSource {
			continue
		}

		sourceVal, diags := sourceAttr.Expr.Value(&hcl.EvalContext{})
		if diags.HasErrors() {
			continue
		}

		if sourceVal.Type() != cty.String {
			continue
		}

		source := sourceVal.AsString()

		// Check if this is a Git-based module
		if !isGitModule(source) {
			continue
		}

		// Extract version from source URL
		version := extractVersion(source)
		if version == "" {
			continue
		}

		// Remove the ?ref= part from source for cleaner comparison
		cleanSource := removeVersionFromSource(source)

		deps = append(deps, ModuleDependency{
			Name:     moduleName,
			Source:   cleanSource,
			Version:  version,
			FilePath: filePath,
			Line:     block.DefRange.Start.Line,
		})
	}

	return deps, nil
}

// scanWithRegex is a fallback parser using regex for cases where HCL parsing fails
func scanWithRegex(content, filePath string) ([]ModuleDependency, error) {
	var deps []ModuleDependency

	// Match module blocks with source attribute
	modulePattern := regexp.MustCompile(`(?s)module\s+"([^"]+)"\s*\{[^}]*source\s*=\s*"([^"]+)"`)
	matches := modulePattern.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		moduleName := content[match[2]:match[3]]
		source := content[match[4]:match[5]]

		if !isGitModule(source) {
			continue
		}

		version := extractVersion(source)
		if version == "" {
			continue
		}

		cleanSource := removeVersionFromSource(source)

		// Calculate line number
		lineNum := strings.Count(content[:match[0]], "\n") + 1

		deps = append(deps, ModuleDependency{
			Name:     moduleName,
			Source:   cleanSource,
			Version:  version,
			FilePath: filePath,
			Line:     lineNum,
		})
	}

	return deps, nil
}

// isGitModule checks if the source URL is a Git-based module
func isGitModule(source string) bool {
	return strings.HasPrefix(source, "git::") ||
		strings.HasPrefix(source, "git@") ||
		strings.Contains(source, "github.com") ||
		strings.Contains(source, "gitlab.com") ||
		strings.Contains(source, "bitbucket.org") ||
		strings.Contains(source, "dev.azure.com") ||
		strings.Contains(source, "_git/")
}

// extractVersion extracts the version/tag from a Git module source
func extractVersion(source string) string {
	// Pattern: ?ref=v1.2.3 or ?ref=1.2.3
	refPattern := regexp.MustCompile(`\?ref=([^&\s]+)`)
	if matches := refPattern.FindStringSubmatch(source); len(matches) > 1 {
		return matches[1]
	}

	// Pattern: .git?ref=tag or //path?ref=tag
	refPattern2 := regexp.MustCompile(`ref=([^&\s"]+)`)
	if matches := refPattern2.FindStringSubmatch(source); len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// removeVersionFromSource removes the ?ref= parameter from source URL
func removeVersionFromSource(source string) string {
	// Remove ?ref=... part
	refPattern := regexp.MustCompile(`\?ref=[^&\s"]+`)
	return refPattern.ReplaceAllString(source, "")
}

// GetModuleSourceBase extracts the base URL without any path or ref
func GetModuleSourceBase(source string) string {
	// Remove git:: prefix
	source = strings.TrimPrefix(source, "git::")

	// Remove ?ref= and anything after
	if idx := strings.Index(source, "?"); idx != -1 {
		source = source[:idx]
	}

	// Remove //path suffix
	if idx := strings.Index(source, "//"); idx != -1 {
		source = source[:idx]
	}

	return source
}

// BuildSourceWithVersion creates a source URL with a specific version
func BuildSourceWithVersion(source, version string) string {
	// Remove existing ref parameter
	cleanSource := removeVersionFromSource(source)

	// Add new version
	if strings.Contains(cleanSource, "?") {
		return fmt.Sprintf("%s&ref=%s", cleanSource, version)
	}
	return fmt.Sprintf("%s?ref=%s", cleanSource, version)
}
