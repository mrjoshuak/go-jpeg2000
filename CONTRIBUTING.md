# Contributing to go-jpeg2000

Thank you for your interest in contributing to go-jpeg2000! This document provides guidelines and information for contributors.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## How to Contribute

### Reporting Bugs

Before creating a bug report, please check existing issues to avoid duplicates. When creating a bug report, include:

- A clear, descriptive title
- Steps to reproduce the issue
- Expected vs actual behavior
- Go version (`go version`)
- Operating system and architecture
- Sample image files if applicable (or describe the image characteristics)

### Suggesting Features

Feature suggestions are welcome! Please include:

- A clear description of the feature
- Use cases and benefits
- Any relevant JPEG 2000 standard references (ISO/IEC 15444)

### Pull Requests

1. **Fork and Clone**: Fork the repository and clone your fork locally.

2. **Branch**: Create a feature branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```

3. **Develop**: Make your changes following the coding standards below.

4. **Test**: Ensure all tests pass:
   ```bash
   go test ./...
   go test -race ./...
   ```

5. **Coverage**: Maintain or improve test coverage (target: 90%+):
   ```bash
   go test -cover ./...
   ```

6. **Commit**: Write clear commit messages:
   ```
   Short summary (50 chars or less)

   More detailed explanation if necessary. Wrap at 72 characters.
   Explain the problem and why this change is needed.

   - Bullet points are fine
   - Use imperative mood ("Add feature" not "Added feature")
   ```

7. **Push and PR**: Push to your fork and create a pull request.

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Run `go fmt` before committing
- Run `go vet` to check for issues
- Use `golint` or `staticcheck` for additional linting

### Code Organization

- Public API belongs in the root package
- Internal implementation goes in `internal/` subdirectories
- Keep packages focused and cohesive
- Prefer composition over inheritance

### Documentation

- All exported functions, types, and constants must have doc comments
- Use complete sentences starting with the item name
- Include examples for complex functionality

```go
// Decode reads a JPEG 2000 image from r and returns it as an image.Image.
// The returned image type depends on the source colorspace and precision.
func Decode(r io.Reader) (image.Image, error) {
```

### Testing

- Write table-driven tests where applicable
- Include both positive and negative test cases
- Test edge cases and error conditions
- Use meaningful test names

```go
func TestDecode_InvalidHeader(t *testing.T) {
    // ...
}
```

### Performance

- Avoid premature optimization
- Use benchmarks to measure performance changes
- Document any performance-critical code
- Prefer clarity over micro-optimizations

## Development Setup

### Prerequisites

- Go 1.21 or later
- Git

### Building

```bash
git clone https://github.com/mrjoshuak/go-jpeg2000.git
cd go-jpeg2000
go build ./...
```

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# With race detection
go test -race ./...

# Specific package
go test ./internal/entropy/

# Verbose
go test -v ./...
```

### Running Benchmarks

```bash
go test -bench=. ./...
go test -bench=. -benchmem ./...
```

### Fuzz Testing

```bash
go test -fuzz=FuzzDecode -fuzztime=60s
go test -fuzz=FuzzHTDecode ./internal/entropy/ -fuzztime=60s
```

## Project Structure

```
go-jpeg2000/
├── jpeg2000.go          # Public API
├── decoder.go           # Decoding implementation
├── encoder.go           # Encoding implementation
├── colorspace.go        # Color conversion
└── internal/
    ├── bio/             # Bit I/O
    ├── box/             # JP2 box parsing
    ├── codestream/      # J2K markers
    ├── dwt/             # Wavelet transform
    ├── entropy/         # MQ coder, EBCOT, HTJ2K
    ├── mct/             # Color transform
    └── tcd/             # Tile coding
```

## JPEG 2000 Standards Reference

When implementing features, refer to:

- **ISO/IEC 15444-1** - Core coding system
- **ISO/IEC 15444-15** - HTJ2K (High-Throughput JPEG 2000)
- **ITU-T Rec. T.800** - Equivalent to Part 1
- **OpenJPEG source** - Reference implementation

## Release Process

Releases are managed by maintainers. Version numbers follow [Semantic Versioning](https://semver.org/):

- MAJOR: Incompatible API changes
- MINOR: New functionality, backward compatible
- PATCH: Bug fixes, backward compatible

## Getting Help

- Open an issue for questions
- Check existing issues and documentation
- Reference the JPEG 2000 standards for codec behavior questions

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
