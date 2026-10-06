package csharp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	csUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
)

// csproj declares packages every way MSBuild allows: a version attribute, a
// version element, an override of a centrally managed version, an Update
// item, and a version taken from a property.
const csproj = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
    <PackageReference Include="Serilog">
      <Version>3.0.0</Version>
    </PackageReference>
    <PackageReference Include="Polly" VersionOverride="8.2.0" />
    <PackageReference Update="xunit" Version="2.6.0" />
    <PackageReference Include="Internal.Package" Version="$(InternalVersion)" />
  </ItemGroup>
</Project>
`

// centralPackages manages versions for every project in the repository.
const centralPackages = `<Project>
  <ItemGroup>
    <PackageVersion Include="Polly" Version="8.0.0" />
    <PackageVersion Include="Dapper" Version="2.1.24" />
  </ItemGroup>
</Project>
`

func TestDeclaredPackageVersions(t *testing.T) {
	t.Parallel()

	t.Run("should read every literal version a project declares", func(t *testing.T) {
		t.Parallel()

		// given / when
		versions, err := csUpdater.DeclaredPackageVersions([]byte(csproj))

		// then
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"Newtonsoft.Json": "13.0.1",
			"Serilog":         "3.0.0",
			"Polly":           "8.2.0",
			"xunit":           "2.6.0",
		}, versions)
	})

	t.Run("should fail on a project it cannot parse", func(t *testing.T) {
		t.Parallel()

		// given / when
		versions, err := csUpdater.DeclaredPackageVersions([]byte("<Project><ItemGroup>"))

		// then
		require.Error(t, err)
		assert.Nil(t, versions)
	})
}

func TestObservePackageChanges(t *testing.T) {
	t.Parallel()

	t.Run("should report project, central and SDK versions a run moved", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{
			"src/App/App.csproj":       csproj,
			"Directory.Packages.props": centralPackages,
			"global.json":              `{"sdk":{"version":"8.0.100"}}`,
		})
		gitrepo.Write(t, root, map[string]string{
			"src/App/App.csproj":       strings.Replace(csproj, `"13.0.1"`, `"13.0.3"`, 1),
			"Directory.Packages.props": strings.Replace(centralPackages, `"2.1.24"`, `"2.1.35"`, 1),
			"global.json":              `{"sdk":{"version":"8.0.404"}}`,
		})

		// when
		changes, err := csUpdater.ObservePackageChanges(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.ElementsMatch(t, []entities.DependencyChange{
			{Subject: entities.SubjectNuGetPackage, Name: "Newtonsoft.Json", From: "13.0.1", To: "13.0.3"},
			{Subject: entities.SubjectNuGetPackage, Name: "Dapper", From: "2.1.24", To: "2.1.35"},
			{Subject: entities.SubjectDotnetVersion, From: "8.0.100", To: "8.0.404"},
		}, changes)
	})
}
