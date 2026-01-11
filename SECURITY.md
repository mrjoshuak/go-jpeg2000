# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x     | :white_check_mark: |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability in go-jpeg2000, please report it responsibly.

### How to Report

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please use GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](../../security) of this repository
2. Click "Report a vulnerability"
3. Fill out the form with details about the vulnerability

### What to Include

When reporting a vulnerability, please include:

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact of the vulnerability
- Any suggested fixes (if you have them)

### Response Timeline

- We will acknowledge receipt of your report within 48 hours
- We will provide an initial assessment within 7 days
- We will work with you to understand and resolve the issue

### Disclosure Policy

- We will coordinate with you on disclosure timing
- We will credit reporters in security advisories (unless you prefer anonymity)
- We ask that you give us reasonable time to address the issue before public disclosure

## Security Best Practices

When using go-jpeg2000:

- Always validate JPEG 2000 files from untrusted sources
- Be aware that malformed JP2/J2K files could cause excessive memory allocation
- Use appropriate resource limits when processing files from untrusted sources

## Scope

This security policy applies to the go-jpeg2000 library code. Issues in the JPEG 2000 specification (ISO/IEC 15444) should be reported to the appropriate standards body.
