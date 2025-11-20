# CI/CD Workflow Documentation

This document describes the Continuous Integration and Continuous Deployment (CI/CD) workflow for the Traefik GeoBlock Plugin.

## Overview

The CI/CD pipeline automatically tests, validates, and builds the plugin whenever code is pushed to the repository or a pull request is created. This ensures code quality, security, and compatibility across different Go versions and platforms.

## Workflow Triggers

The CI pipeline is triggered on:

- **Push events** to branches:
  - `main`
  - `master`
  - `develop`
  - `claude/**` (for AI-assisted development branches)

- **Pull request events** targeting:
  - `main`
  - `master`
  - `develop`

## Pipeline Jobs

### 1. Lint Job

**Purpose**: Ensures code quality and consistency using static analysis.

**What it does**:
- Checks out the repository code
- Sets up Go 1.21 environment
- Runs `golangci-lint` with configuration from `.golangci.yml`
- Validates code against 25+ linters including:
  - Error checking (errcheck, gosec)
  - Code simplification (gosimple, gocritic)
  - Formatting (gofmt, gofumpt, goimports)
  - Complexity analysis (cyclop, funlen)
  - Security scanning (gosec)

**Configuration**: See `.golangci.yml` for detailed linter settings.

### 2. Test Job

**Purpose**: Runs unit tests across multiple Go versions to ensure compatibility.

**What it does**:
- Tests on Go versions: 1.21, 1.22, 1.23
- Downloads and verifies dependencies
- Runs tests with race detection enabled
- Generates code coverage reports
- Uploads coverage to Codecov (for Go 1.21 only)

**Commands executed**:
```bash
go mod download
go mod verify
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -func=coverage.out
```

**Test coverage includes**:
- Configuration validation
- IP detection (private/public)
- Client IP extraction from headers
- Country blocking logic
- Cache functionality
- HTTP request handling

### 3. Validate Plugin Job

**Purpose**: Validates the Traefik plugin manifest file.

**What it does**:
- Checks that `.traefik.yml` exists
- Validates required fields:
  - `displayName`: Plugin display name
  - `type`: Plugin type (should be "middleware")
- Ensures the plugin can be registered in Traefik's plugin catalog

### 4. Build Job

**Purpose**: Verifies the plugin compiles across different platforms.

**What it does**:
- Builds the plugin for multiple OS/architecture combinations:
  - Linux: amd64, arm64
  - macOS (darwin): amd64, arm64
  - Windows: amd64
- Uses compiler optimizations: `-ldflags="-s -w"` to reduce binary size
- Runs only after lint, test, and validate jobs succeed

**Build matrix**:
| OS      | Architecture | Status |
|---------|-------------|--------|
| Linux   | amd64       | ✓      |
| Linux   | arm64       | ✓      |
| macOS   | amd64       | ✓      |
| macOS   | arm64       | ✓      |
| Windows | amd64       | ✓      |

### 5. Security Scan Job

**Purpose**: Identifies potential security vulnerabilities in the code.

**What it does**:
- Runs Gosec security scanner
- Generates SARIF format report
- Uploads results to GitHub Security tab
- Continues even if vulnerabilities are found (non-blocking)

**Security checks include**:
- SQL injection vulnerabilities
- Command injection risks
- Hardcoded credentials
- Weak cryptography
- File permission issues
- Integer overflow

### 6. CI Success Job

**Purpose**: Provides a single status indicator for all pipeline jobs.

**What it does**:
- Waits for all other jobs to complete
- Checks the status of each job
- Fails if any required job fails
- Provides a clear success/failure message

## Configuration Files

### .golangci.yml

Configures the golangci-lint tool with 25+ linters:

**Key settings**:
- Go version: 1.21
- Timeout: 5 minutes
- Auto-fix: Enabled for fixable issues
- Maximum cyclomatic complexity: 20
- Maximum function length: 100 lines / 50 statements

**Linter categories**:
1. **Error detection**: errcheck, govet, staticcheck
2. **Code quality**: gosimple, unused, gocritic
3. **Formatting**: gofmt, gofumpt, goimports, gci
4. **Security**: gosec
5. **Complexity**: cyclop, funlen
6. **Best practices**: nilnil, unconvert, unparam, usestdlibvars

### .github/workflows/ci.yml

Main CI workflow configuration file. See "Pipeline Jobs" section above for details.

## Running CI Checks Locally

### Prerequisites

Install required tools:

```bash
# Install golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Install gosec (optional)
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### Run Linting

```bash
# Run all linters
golangci-lint run

# Run with auto-fix
golangci-lint run --fix

# Run specific linters
golangci-lint run --disable-all --enable=errcheck,govet,staticcheck
```

### Run Tests

```bash
# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out  # View coverage in browser
```

### Run Security Scan

```bash
# Run gosec
gosec ./...

# Generate SARIF report
gosec -fmt sarif -out gosec-results.sarif ./...
```

### Validate Plugin Manifest

```bash
# Check .traefik.yml exists and has required fields
cat .traefik.yml | grep -E "(displayName|type):"
```

### Build for Multiple Platforms

```bash
# Build for Linux amd64
GOOS=linux GOARCH=amd64 go build -v -ldflags="-s -w" .

# Build for macOS arm64
GOOS=darwin GOARCH=arm64 go build -v -ldflags="-s -w" .

# Build for Windows amd64
GOOS=windows GOARCH=amd64 go build -v -ldflags="-s -w" .
```

## Continuous Integration Best Practices

### 1. Commit Frequently

Make small, focused commits that pass all CI checks. This makes debugging easier when something breaks.

### 2. Fix Linting Issues Promptly

Don't let linting issues accumulate. Many can be auto-fixed:

```bash
golangci-lint run --fix
```

### 3. Write Tests for New Features

Maintain or improve code coverage with every change:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### 4. Check CI Status Before Merging

Always ensure all CI jobs pass before merging pull requests. The "CI Success" job provides a single status check.

### 5. Keep Dependencies Updated

Regularly update dependencies and ensure tests still pass:

```bash
go get -u ./...
go mod tidy
go test ./...
```

## Troubleshooting

### Lint Failures

**Problem**: golangci-lint reports errors

**Solutions**:
1. Run `golangci-lint run --fix` to auto-fix issues
2. Check specific linter documentation if auto-fix doesn't work
3. Add exclusion rules to `.golangci.yml` if needed (use sparingly)

### Test Failures

**Problem**: Tests fail in CI but pass locally

**Solutions**:
1. Ensure you're using the correct Go version (1.21+)
2. Run tests with race detection: `go test -race ./...`
3. Check for environment-specific issues
4. Verify all dependencies are committed

### Build Failures

**Problem**: Build fails for specific platforms

**Solutions**:
1. Test build locally with target platform:
   ```bash
   GOOS=linux GOARCH=amd64 go build .
   ```
2. Check for platform-specific code that needs build tags
3. Ensure all dependencies support target platform

### Coverage Drops

**Problem**: Code coverage decreases

**Solutions**:
1. Add tests for new code
2. Check coverage report: `go tool cover -html=coverage.out`
3. Identify untested code paths
4. Write table-driven tests for comprehensive coverage

## Extending the CI Pipeline

### Adding New Linters

Edit `.golangci.yml`:

```yaml
linters:
  enable:
    - your-new-linter
```

See [golangci-lint linters](https://golangci-lint.run/usage/linters/) for available options.

### Adding New Test Platforms

Edit `.github/workflows/ci.yml` test matrix:

```yaml
strategy:
  matrix:
    go-version: ['1.21', '1.22', '1.23', '1.24']  # Add new version
```

### Adding New Build Targets

Edit `.github/workflows/ci.yml` build matrix:

```yaml
strategy:
  matrix:
    goos: [linux, darwin, windows, freebsd]  # Add freebsd
    goarch: [amd64, arm64, arm]  # Add arm
```

### Adding Integration Tests

Create a new job in `.github/workflows/ci.yml`:

```yaml
integration-test:
  name: Integration Tests
  runs-on: ubuntu-latest
  steps:
    - name: Checkout code
      uses: actions/checkout@v4

    - name: Start test services
      run: docker-compose up -d

    - name: Run integration tests
      run: go test -tags=integration ./...
```

## Status Badges

Add these badges to your README.md to show CI status:

```markdown
![CI Status](https://github.com/CangioUni/traefik-geoblock-plugin/workflows/CI/badge.svg)
![Go Version](https://img.shields.io/github/go-mod/go-version/CangioUni/traefik-geoblock-plugin)
![License](https://img.shields.io/github/license/CangioUni/traefik-geoblock-plugin)
```

## Additional Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [golangci-lint Documentation](https://golangci-lint.run/)
- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Traefik Plugin Documentation](https://doc.traefik.io/traefik-pilot/plugins/plugin-dev/)
- [Gosec Security Scanner](https://github.com/securego/gosec)

## Support

If you encounter issues with the CI/CD pipeline:

1. Check the [GitHub Actions logs](https://github.com/CangioUni/traefik-geoblock-plugin/actions)
2. Run the failing command locally to reproduce
3. Review this documentation for troubleshooting steps
4. Open an issue with the error details and steps to reproduce
