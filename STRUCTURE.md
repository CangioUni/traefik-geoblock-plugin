# Project Structure

This document explains the organization of the Traefik GeoBlock plugin project.

## Directory Tree

```
traefik-geoblock-plugin/
├── .github/
│   └── workflows/
│       └── ci.yml                    # GitHub Actions CI/CD pipeline
├── .gitignore                        # Git ignore rules
├── .traefik.yml                      # Traefik plugin manifest (required)
├── geoblock.go                       # Main plugin implementation
├── geoblock_test.go                  # Unit tests
├── go.mod                            # Go module definition
├── LICENSE                           # MIT License
├── Makefile                          # Development commands
├── README.md                         # Main documentation
├── QUICKSTART.md                     # 5-minute getting started guide
├── PROJECT-SUMMARY.md                # Project overview and features
├── DEPLOYMENT-CHECKLIST.md           # Step-by-step deployment guide
├── STRUCTURE.md                      # This file
├── docker-compose.yml                # Docker Compose for local testing
├── traefik-static-config.yml         # Example static configuration
├── traefik-dynamic-config.yml        # Example dynamic configuration
└── advanced-example.yml              # Advanced usage scenarios
```

## File Descriptions

### Core Plugin Files

#### `.traefik.yml`
**Purpose**: Required Traefik plugin manifest file  
**When to modify**: Never (unless changing plugin metadata)  
**Contains**: Plugin display name, type, import path, and test configuration

#### `geoblock.go`
**Purpose**: Main plugin implementation  
**When to modify**: When adding features or fixing bugs  
**Contains**: 
- Plugin configuration structure
- HTTP middleware handler
- IP extraction logic
- GeoIP API integration
- Caching mechanism
- Blocking logic

#### `geoblock_test.go`
**Purpose**: Comprehensive unit tests  
**When to modify**: When adding new features (add corresponding tests)  
**Contains**:
- Configuration tests
- IP detection tests
- Private IP detection tests
- Blocking logic tests
- Cache tests

#### `go.mod`
**Purpose**: Go module definition  
**When to modify**: After creating GitHub repo (update module path)  
**Contains**: Module name and Go version

### Documentation Files

#### `README.md`
**Purpose**: Complete project documentation  
**Audience**: All users  
**Contains**:
- Feature overview
- Configuration options
- Usage examples
- Installation instructions
- Troubleshooting guide

#### `QUICKSTART.md`
**Purpose**: Fast-track guide for quick deployment  
**Audience**: Users who want to get started immediately  
**Contains**:
- Minimal setup steps
- Common use cases
- Basic troubleshooting

#### `PROJECT-SUMMARY.md`
**Purpose**: High-level overview and deployment guide  
**Audience**: Project managers, technical leads  
**Contains**:
- What's included in the package
- Key features summary
- Deployment steps
- Use cases

#### `DEPLOYMENT-CHECKLIST.md`
**Purpose**: Step-by-step deployment verification  
**Audience**: DevOps engineers, system administrators  
**Contains**:
- Pre-deployment checks
- Configuration steps
- Testing procedures
- Post-deployment tasks

#### `STRUCTURE.md`
**Purpose**: Explain project organization  
**Audience**: Contributors, developers  
**Contains**:
- File descriptions
- Modification guidelines
- Project organization

### Configuration Examples

#### `traefik-static-config.yml`
**Purpose**: Example static configuration  
**When to use**: Setting up Traefik with the plugin  
**Contains**:
- Plugin registration
- Entry points
- Provider configuration

#### `traefik-dynamic-config.yml`
**Purpose**: Example dynamic configuration  
**When to use**: Creating middleware and applying to routers  
**Contains**:
- 3 middleware examples (allowlist, blocklist, strict)
- Router examples
- Service definitions

#### `advanced-example.yml`
**Purpose**: Complex usage scenarios  
**When to use**: Advanced deployments  
**Contains**:
- Geo-based rate limiting
- Progressive restrictions
- Compliance configurations (GDPR)
- Multi-middleware combinations

#### `docker-compose.yml`
**Purpose**: Local testing environment  
**When to use**: Testing the plugin locally  
**Contains**:
- Traefik service configuration
- Example backend service (whoami)
- Network setup

### Development Files

#### `Makefile`
**Purpose**: Common development tasks  
**When to use**: Development and testing  
**Available commands**:
```bash
make test      # Run unit tests
make lint      # Run linter
make format    # Format code
make check     # Check formatting
make build     # Verify build
make clean     # Clean artifacts
make coverage  # Show test coverage
```

#### `.github/workflows/ci.yml`
**Purpose**: Automated testing and validation  
**When it runs**: On push, PR, and releases  
**Contains**:
- Test workflow (multiple Go versions)
- Linting checks
- Format verification
- Build validation

#### `.gitignore`
**Purpose**: Exclude files from version control  
**When to modify**: Adding new build artifacts or IDE files  
**Contains**:
- Compiled binaries
- Test output
- IDE files
- OS-specific files

#### `LICENSE`
**Purpose**: Legal terms (MIT License)  
**When to modify**: If changing license terms  
**Contains**: MIT License text

## How Files Work Together

### Development Workflow
```
1. Modify geoblock.go
2. Run: make format
3. Run: make test
4. Run: make lint
5. Commit changes
6. GitHub Actions runs CI
```

### Deployment Workflow
```
1. Update go.mod with your GitHub path
2. Push to GitHub
3. Create git tag (v0.1.0)
4. Update traefik-static-config.yml
5. Update traefik-dynamic-config.yml
6. Restart Traefik
```

### Testing Workflow
```
1. Start: docker-compose up -d
2. Test: curl http://whoami.localhost
3. Check logs: docker-compose logs traefik
4. Modify configuration as needed
5. Test again
```

## What to Modify

### Before First Deployment
✏️ **Must modify**:
- `go.mod` - Replace `yourusername` with your GitHub username

🔧 **Should modify**:
- `traefik-static-config.yml` - Update module name
- `traefik-dynamic-config.yml` - Customize middleware settings
- Country lists in middleware configurations

📝 **Optional**:
- `README.md` - Add your specific use case
- `docker-compose.yml` - Adjust for your testing needs

### Never Modify (unless you know what you're doing)
- `.traefik.yml` - Required by Traefik
- `LICENSE` - Unless changing license
- GitHub Actions workflow (unless customizing CI)

## File Sizes (Approximate)

```
geoblock.go              ~7.5 KB   (Core implementation)
geoblock_test.go         ~6.5 KB   (Comprehensive tests)
README.md                ~7.5 KB   (Full documentation)
PROJECT-SUMMARY.md       ~6 KB     (Overview)
DEPLOYMENT-CHECKLIST.md  ~5.5 KB   (Step-by-step guide)
QUICKSTART.md            ~4.5 KB   (Quick start)
advanced-example.yml     ~5 KB     (Complex examples)
traefik-dynamic-config.yml ~2.5 KB (Basic examples)
Other files              <2 KB each
```

## Dependencies

### Runtime Dependencies
- **None** - Plugin uses only Go standard library

### Development Dependencies
- Go 1.21+ (for testing and building)
- golangci-lint (for linting, optional)
- make (for Makefile commands, optional)

### External Services
- GeoIP API (default: ipapi.co)
  - Free tier: 30,000 requests/month
  - Can be replaced with any compatible service

## Adding New Features

1. **Write code**: Modify `geoblock.go`
2. **Write tests**: Add to `geoblock_test.go`
3. **Update docs**: Modify `README.md` and examples
4. **Test locally**: `make test && make lint`
5. **Update version**: Create new git tag (e.g., v0.2.0)

## Version Tagging Strategy

- `v0.1.x` - Bug fixes and patches
- `v0.2.x` - Minor features, backward compatible
- `v1.x.x` - Major version, may have breaking changes

## Questions?

- Check README.md for detailed documentation
- Review QUICKSTART.md for quick examples
- See PROJECT-SUMMARY.md for overview
- Follow DEPLOYMENT-CHECKLIST.md for deployment

---

**Tip**: Start by reading QUICKSTART.md, then refer to README.md for detailed configuration options.
