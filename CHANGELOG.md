# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Server configuration loading from file
- Default config values from dev.yaml
- Unit tests for config file loading
- Added local minio docker setup
- Updated README.md and justfile
- Initial server implementation with MinIO blob storage support
- TLS support with certificate configuration
- YAML-based configuration system
- Basic file permissions system using `syft.pub.yaml` files
- File sync client for local directory synchronization
- Development tooling via justfile commands
- Logging using slog
- Signal handling for graceful shutdown
