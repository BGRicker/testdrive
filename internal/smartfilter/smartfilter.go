package smartfilter

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bgricker/testdrive/internal/config"
	"github.com/bmatcuk/doublestar/v4"
)

// FindRelatedTests finds test files related to the given changed files.
// It uses the smart filter rules to map source files to their corresponding tests.
func FindRelatedTests(root string, changedFiles []string, rules []config.SmartFilterRule) ([]string, error) {
	relatedTests := make(map[string]bool)

	for _, file := range changedFiles {
		// Convert to relative path
		relPath, err := filepath.Rel(root, file)
		if err != nil {
			relPath = file
		}

		// If the changed file is already a test file, include it
		if isTestFile(relPath) {
			relatedTests[file] = true
			continue
		}

		// Find matching rules and add related test files
		for _, rule := range rules {
			matched, err := doublestar.Match(rule.Pattern, relPath)
			if err != nil || !matched {
				continue
			}

			// Map source file to test file using the rule
			testFiles := mapSourceToTests(root, relPath, rule)
			for _, testFile := range testFiles {
				relatedTests[testFile] = true
			}

			// Add additional test patterns if specified
			for _, additionalPattern := range rule.Additional {
				additionalTests := findTestsByPattern(root, relPath, additionalPattern)
				for _, testFile := range additionalTests {
					relatedTests[testFile] = true
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(relatedTests))
	for testFile := range relatedTests {
		result = append(result, testFile)
	}

	return result, nil
}

// isTestFile checks if a file is already a test file.
func isTestFile(path string) bool {
	patterns := []string{
		"**/*_test.go",
		"**/*_spec.rb",
		"**/*.test.js",
		"**/*.test.ts",
		"**/*.test.jsx",
		"**/*.test.tsx",
		"**/*.spec.js",
		"**/*.spec.ts",
		"**/*.spec.jsx",
		"**/*.spec.tsx",
		"**/test_*.py",
		"**/*_test.py",
		"spec/**/*_spec.rb",
	}

	for _, pattern := range patterns {
		matched, err := doublestar.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// mapSourceToTests maps a source file to its test file(s) based on a rule.
func mapSourceToTests(root, sourcePath string, rule config.SmartFilterRule) []string {
	// Extract the directory and filename parts
	dir := filepath.Dir(sourcePath)
	base := filepath.Base(sourcePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)

	// Determine test file name based on language conventions
	var testFileName string
	if ext == ".rb" {
		testFileName = nameWithoutExt + "_spec.rb"
	} else if ext == ".go" {
		testFileName = nameWithoutExt + "_test.go"
	} else if ext == ".js" || ext == ".jsx" {
		testFileName = nameWithoutExt + ".test" + ext
	} else if ext == ".ts" || ext == ".tsx" {
		testFileName = nameWithoutExt + ".test" + ext
	} else if ext == ".py" {
		testFileName = "test_" + nameWithoutExt + ext
	} else {
		testFileName = nameWithoutExt + "_test" + ext
	}

	// Map directory structure (e.g., app/models -> spec/models)
	testDir := mapDirectory(dir, rule)

	// Construct the test file path
	testPath := filepath.Join(root, testDir, testFileName)

	// Check if the test file exists
	if _, err := os.Stat(testPath); err == nil {
		return []string{testPath}
	}

	return []string{}
}

// mapDirectory maps a source directory to its test directory based on conventions.
func mapDirectory(sourceDir string, rule config.SmartFilterRule) string {
	// Rails conventions
	if strings.HasPrefix(sourceDir, "app/") {
		return strings.Replace(sourceDir, "app/", "spec/", 1)
	}
	if strings.HasPrefix(sourceDir, "lib/") {
		return strings.Replace(sourceDir, "lib/", "spec/lib/", 1)
	}

	// Go conventions (tests in same directory)
	if strings.HasSuffix(rule.TestPattern, "*_test.go") {
		return sourceDir
	}

	// JavaScript/TypeScript conventions
	if strings.HasPrefix(sourceDir, "src/") {
		// Tests might be in same directory or in tests/ directory
		return sourceDir
	}

	// Python conventions
	if rule.TestPattern != "" && strings.Contains(rule.TestPattern, "test_") {
		// Python tests are often in a tests/ directory mirroring source layout.
		if strings.HasPrefix(sourceDir, "tests/") {
			return sourceDir
		}
		if strings.HasPrefix(sourceDir, "src/") {
			return strings.Replace(sourceDir, "src/", "tests/", 1)
		}
		if strings.HasPrefix(sourceDir, "lib/") {
			return strings.Replace(sourceDir, "lib/", "tests/lib/", 1)
		}
		return filepath.Join("tests", sourceDir)
	}

	return sourceDir
}

// findTestsByPattern finds test files matching a pattern relative to the source file.
func findTestsByPattern(root, _ string, pattern string) []string {
	var results []string

	matches, err := doublestar.Glob(os.DirFS(root), pattern)
	if err != nil {
		return results
	}

	for _, relPath := range matches {
		if !isTestFile(relPath) {
			continue
		}
		results = append(results, filepath.Join(root, relPath))
	}

	return results
}

// IsLinterCommand checks if a command is a linter (not a test).
func IsLinterCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}

	tokens := strings.Fields(lower)
	hasStandard := false
	hasRubocop := false
	hasEslint := false
	for _, tok := range tokens {
		if isLinterToken(tok, "standard") || isLinterToken(tok, "standardrb") {
			hasStandard = true
		}
		if isLinterToken(tok, "rubocop") {
			hasRubocop = true
		}
		if isLinterToken(tok, "eslint") {
			hasEslint = true
		}
	}

	hasYarnLint := strings.HasPrefix(lower, "yarn lint") || strings.HasPrefix(lower, "yarn run lint")
	hasNpmLint := strings.HasPrefix(lower, "npm run lint")

	return hasStandard || hasRubocop || hasEslint || hasYarnLint || hasNpmLint
}

func isLinterToken(token, name string) bool {
	if token == name {
		return true
	}

	lastSlash := strings.LastIndexAny(token, "/\\")
	if lastSlash != -1 && lastSlash+1 < len(token) {
		return token[lastSlash+1:] == name
	}
	return false
}

// FormatTestCommand modifies a test command to only run specific test files.
// It detects the test framework and adjusts the command accordingly.
// If the command is not a test command, it returns the original unchanged.
func FormatTestCommand(originalCommand string, testFiles []string, root string) string {
	if len(testFiles) == 0 {
		return originalCommand
	}

	// Detect framework and format command
	lower := strings.ToLower(originalCommand)

	// Exclude pure setup/install commands (but allow compound commands like "db:setup spec")
	isPureSetupCommand := (strings.Contains(lower, "db:migrate") ||
		strings.Contains(lower, "db:create") ||
		strings.Contains(lower, "db:reset") ||
		strings.Contains(lower, "yarn install") ||
		strings.Contains(lower, "npm install") ||
		strings.Contains(lower, "bundle install")) &&
		!strings.Contains(lower, "spec") &&
		!strings.Contains(lower, "test")

	if isPureSetupCommand {
		return originalCommand
	}

	// Check if this is a filterable command (tests only)
	isFilterableCommand := strings.Contains(lower, "rspec") ||
		strings.Contains(lower, "go test") ||
		strings.Contains(lower, "jest") ||
		strings.Contains(lower, "pytest") ||
		strings.Contains(lower, "npm test") ||
		strings.Contains(lower, "yarn test") ||
		(strings.Contains(lower, "rails") && strings.Contains(lower, "spec")) // Rails with spec

	// If not a filterable command, return original unchanged
	if !isFilterableCommand {
		return originalCommand
	}

	// Convert absolute paths to relative paths
	relPaths := make([]string, 0, len(testFiles))
	for _, file := range testFiles {
		relPath, err := filepath.Rel(root, file)
		if err != nil {
			relPath = file
		}
		relPaths = append(relPaths, relPath)
	}

	filesStr := strings.Join(relPaths, " ")

	// Rails db:setup spec - in watch mode, skip db:setup and just run rspec.
	// Restrict to simple invocations to avoid dropping db:setup in compound commands.
	if strings.Contains(lower, "rails") &&
		strings.Contains(lower, "db:setup") &&
		(strings.Contains(lower, " spec") || strings.HasSuffix(lower, "spec")) &&
		!strings.Contains(lower, "rspec") &&
		!strings.Contains(lower, "&&") &&
		!strings.Contains(lower, ";") &&
		!strings.Contains(lower, "|") {
		return "bundle exec rspec " + filesStr
	}

	// RSpec (Rails) - matches both `rspec` and `bundle exec rspec`.
	if strings.Contains(lower, "rspec") {
		// Replace generic "spec" with specific files
		if strings.Contains(originalCommand, " spec") || strings.HasSuffix(originalCommand, "spec") {
			return strings.Replace(originalCommand, " spec", " "+filesStr, 1)
		}
		return originalCommand + " " + filesStr
	}

	// Go tests
	if strings.Contains(lower, "go test") {
		// Extract package paths from test files
		packages := make(map[string]bool)
		for _, file := range testFiles {
			pkgDir := filepath.Dir(file)
			relPkg, err := filepath.Rel(root, pkgDir)
			if err != nil {
				relPkg = pkgDir
			}
			packages[relPkg] = true
		}
		pkgList := make([]string, 0, len(packages))
		for pkg := range packages {
			if pkg == "." {
				pkgList = append(pkgList, ".")
				continue
			}
			if filepath.IsAbs(pkg) {
				pkgList = append(pkgList, pkg)
				continue
			}
			pkgList = append(pkgList, "./"+pkg)
		}
		return "go test " + strings.Join(pkgList, " ")
	}

	// Jest/npm test
	if strings.Contains(lower, "jest") || strings.Contains(lower, "npm test") {
		return originalCommand + " -- " + filesStr
	}

	// pytest
	if strings.Contains(lower, "pytest") {
		if strings.Contains(originalCommand, " tests") || strings.HasSuffix(originalCommand, "pytest") {
			return strings.Replace(originalCommand, " tests", " "+filesStr, 1)
		}
		return originalCommand + " " + filesStr
	}

	// Default: append files to the end
	return originalCommand + " " + filesStr
}
