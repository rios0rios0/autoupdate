package java_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
)

// pom declares artifacts every way versions-maven-plugin rewrites them: a
// property shared by two dependencies, a literal version, a managed
// dependency, a plugin without a groupId, a profile and a parent.
const pom = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>3.3.0</version>
  </parent>
  <groupId>com.example</groupId>
  <artifactId>app</artifactId>
  <version>1.0.0</version>
  <properties>
    <java.version>21</java.version>
    <jackson.version>2.17.0</jackson.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>com.fasterxml.jackson.core</groupId>
      <artifactId>jackson-databind</artifactId>
      <version>${jackson.version}</version>
    </dependency>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>33.0.0-jre</version>
    </dependency>
    <dependency>
      <groupId>org.slf4j</groupId>
      <artifactId>slf4j-api</artifactId>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>sibling</artifactId>
      <version>${project.version}</version>
    </dependency>
  </dependencies>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.apache.logging.log4j</groupId>
        <artifactId>log4j-bom</artifactId>
        <version>2.23.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
  <build>
    <plugins>
      <plugin>
        <artifactId>maven-surefire-plugin</artifactId>
        <version>3.2.0</version>
      </plugin>
    </plugins>
  </build>
  <profiles>
    <profile>
      <id>extra</id>
      <properties>
        <commons.version>3.14.0</commons.version>
      </properties>
      <dependencies>
        <dependency>
          <groupId>org.apache.commons</groupId>
          <artifactId>commons-lang3</artifactId>
          <version>${commons.version}</version>
        </dependency>
      </dependencies>
    </profile>
  </profiles>
</project>
`

func TestDeclaredArtifactVersions(t *testing.T) {
	t.Parallel()

	t.Run("should read every artifact a POM declares a version for", func(t *testing.T) {
		t.Parallel()

		// given / when
		versions, err := javaUpdater.DeclaredArtifactVersions([]byte(pom))

		// then: slf4j-api is managed elsewhere and the sibling's version is
		// the project's own, so neither names a version here
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"org.springframework.boot:spring-boot-starter-parent": "3.3.0",
			"com.fasterxml.jackson.core:jackson-databind":         "2.17.0",
			"com.google.guava:guava":                              "33.0.0-jre",
			"org.apache.logging.log4j:log4j-bom":                  "2.23.0",
			"org.apache.maven.plugins:maven-surefire-plugin":      "3.2.0",
			"org.apache.commons:commons-lang3":                    "3.14.0",
		}, versions)
	})

	t.Run("should fail on a POM it cannot parse", func(t *testing.T) {
		t.Parallel()

		// given / when
		versions, err := javaUpdater.DeclaredArtifactVersions([]byte("<project><dependencies>"))

		// then
		require.Error(t, err)
		assert.Nil(t, versions)
	})
}

func TestGradleWrapperVersion(t *testing.T) {
	t.Parallel()

	t.Run("should read the version the wrapper distributes", func(t *testing.T) {
		t.Parallel()

		testCases := map[string]string{
			"distributionUrl=https\\://services.gradle.org/distributions/gradle-8.10.2-bin.zip\n": "8.10.2",
			"distributionUrl=https\\://services.gradle.org/distributions/gradle-9.0-rc-1-all.zip": "9.0-rc-1",
			"distributionBase=GRADLE_USER_HOME\n":                                                 "",
		}

		for content, want := range testCases {
			// when
			version := javaUpdater.GradleWrapperVersion(content)

			// then
			assert.Equal(t, want, version, content)
		}
	})
}

func TestObserveArtifactChanges(t *testing.T) {
	t.Parallel()

	t.Run("should report artifacts across a reactor, the wrapper and the Java version", func(t *testing.T) {
		t.Parallel()

		// given
		wrapper := "distributionUrl=https\\://services.gradle.org/distributions/gradle-8.10-bin.zip\n"
		root := gitrepo.New(t, map[string]string{
			"pom.xml":        pom,
			"module/pom.xml": pom,
			".java-version":  "21\n",
			"gradle/wrapper/gradle-wrapper.properties": wrapper,
		})
		gitrepo.Write(t, root, map[string]string{
			"pom.xml":        strings.Replace(pom, "<jackson.version>2.17.0<", "<jackson.version>2.18.1<", 1),
			"module/pom.xml": strings.Replace(pom, "33.0.0-jre", "33.3.1-jre", 1),
			".java-version":  "25\n",
			"gradle/wrapper/gradle-wrapper.properties": strings.Replace(wrapper, "8.10", "8.11", 1),
		})

		// when
		changes, err := javaUpdater.ObserveArtifactChanges(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.ElementsMatch(t, []entities.DependencyChange{
			{
				Subject: entities.SubjectMavenDependency, Name: "com.fasterxml.jackson.core:jackson-databind",
				From: "2.17.0", To: "2.18.1",
			},
			{
				Subject: entities.SubjectMavenDependency, Name: "com.google.guava:guava",
				From: "33.0.0-jre", To: "33.3.1-jre",
			},
			{Subject: entities.SubjectJavaVersion, From: "21", To: "25"},
			{Subject: entities.SubjectGradleVersion, From: "8.10", To: "8.11"},
		}, changes)
	})
}
