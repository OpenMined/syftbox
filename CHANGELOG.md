# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### [PR #78](https://github.com/OpenMined/syftbox/pull/78) - HTTP Message Header Fixes *(2025-09-12)*

#### Added
- **Guest Email Standardization**: Changed legacy guest email references to use `syftbox.net` domain
- **Test Coverage**: Added comprehensive tests for guest email normalization and header processing
- **Header Filtering**: Authorization header is now filtered out from forwarded requests for security

#### Enhanced
- **Code Maintainability**: Refactored guest email handling to use constants for better maintainability
- **Header Processing**: Enhanced HTTP message header handling for better reliability
- **Request Headers**: All headers are now converted to lowercase for consistency

#### Fixed
- **HTTP Headers**: Resolved issues with HTTP message header processing
- **Guest Email Handling**: Fixed inconsistencies in guest email format across the system
- **Authorization Forwarding**: Prevented Authorization headers from being forwarded to RPC endpoints

#### Documentation
- **Documentation Updates**: Updated curl command examples to reflect new guest email format
- **API Reference**: Enhanced send handler documentation with header filtering details

### [PR #69](https://github.com/OpenMined/syftbox/pull/69) - ACL Integration for RPC Operations *(2025-09-12)*

#### Added
- **Sender Suffix Support**: New `suffix-sender` parameter enables user-partitioned storage for better isolation
- **Dual Path Support**: Automatic fallback between new user-partitioned and legacy shared request paths
- **Enhanced Security**: Comprehensive permission checks for both message sending and polling operations
- **Owner Bypass**: Datasite owners maintain full access to their applications
- **URL Header**: Added `x-syft-url` header to request files for downstream application access

#### Enhanced
- **RPC Security**: All RPC operations now respect ACL permissions with user-specific access control
- **Message Polling**: Improved polling logic with automatic path resolution and smart fallback
- **Request Cleanup**: Unified and simplified request/response file cleanup logic
- **Backward Compatibility**: Seamless migration path from legacy to user-partitioned storage

#### Fixed
- **Path Resolution**: Improved request path detection and validation in polling operations
- **Permission Checks**: Enhanced ACL integration for both request and response access control

#### Documentation
- **Send Handler Guide**: Enhanced documentation with user partitioning details and ACL rules
- **API Examples**: Updated curl commands to include `suffix-sender` parameter
- **Storage Structure**: Added comprehensive examples of ACL rules for various use cases
- **Backward Compatibility**: Documented migration path and legacy support





## [0.8.4] - 2025-09-04

### Added

#### 🔐 Access Logging System
- **Per-User Logs**: Each user has their own log directory with detailed access tracking
- **Structured JSON Logs**: JSON-formatted logs with timestamp, path, access type, IP, user-agent, and status
- **Enhanced User Agent**: Detailed client information including OS version, architecture, and Go runtime
- **Automatic Rotation**: Logs rotate at 10MB with max 5 files per user, stored in `.logs/access/`
- **Security Focused**: 0600 file permissions, no HTTP access, sanitized usernames for safe filesystem paths

#### 🛡️ Advanced ACL (Access Control List) System
- **Path Templates**: Dynamic path matching using template patterns (e.g., `/users/{USER}/files/*`)
- **Computed Rules**: Runtime rule evaluation with user-specific access control
- **Pattern Matching**: Support for glob patterns and regex-based access rules
- **Efficient Caching**: Smart caching system for improved performance on large rule sets
- **Domain-based Access**: Control access based on subdomain patterns and user domains
- **Rule Specificity**: Intelligent rule scoring system for precise access control

#### 🎨 Auth Token Generation UI
- **Web Dashboard**: User-friendly web interface for token generation with email verification
- **OTP Verification**: 8-digit verification code system for secure token generation
- **Environment Variables**: Automatic generation of SYFTBOX_EMAIL, SYFTBOX_SERVER_URL, and SYFTBOX_REFRESH_TOKEN
- **Copy to Clipboard**: One-click copying of environment variables for easy deployment

#### 📚 Enhanced Documentation
- **ACL System Guide**: Comprehensive documentation with real-world examples and use cases
- **Send Handler Documentation**: Detailed API documentation for file sharing endpoints
- **Docker Setup Guides**: Step-by-step instructions for development and production environments
- **Architecture Diagrams**: Visual documentation of system components and data flow
- **E2E Encryption Flow**: Detailed diagrams showing end-to-end encryption process

### Enhanced

#### ⚡ Performance Improvements
- **ACL Caching**: High-performance LRU cache with TTL for rapid access control decisions
- **Memory Optimization**: Efficient memory usage with ~100KB per 1000 files
- **Request Processing**: O(1) cached lookups and O(depth) tree traversal for optimal performance

#### 🛠️ Developer Experience
- **Docker Tooling**: Streamlined container development with VSCode attachment support
- **Configuration Management**: Simplified server configuration with YAML-based settings
- **Testing Infrastructure**: Improved test coverage and reliability for subdomain handling

#### 👥 User Experience
- **Authentication Flow**: Streamlined login process with web-based token generation
- **Error Handling**: Better error messages and user feedback throughout the application
- **Client Connectivity**: Improved online status detection and connection management

### Fixed

#### 🐛 Bug Fixes
- **Subdomain Testing**: Resolved intermittent failures in subdomain routing tests
- **Docker Naming**: Fixed confusing Docker file naming (Docker.prod → Docker.client.ds)
- **Client Connectivity**: Improved online status checking to prevent false offline states
- **Auth UI**: Fixed layout issues and improved responsiveness of authentication interface
- **File Permissions**: Resolved edge cases in file permission validation

#### 🔧 Infrastructure
- **Test Reliability**: Fixed flaky tests in subdomain and ACL functionality
- **Container Builds**: Resolved multi-architecture build issues
- **Log Management**: Fixed log directory creation and permission issues

### Documentation

#### 📖 New Documentation
- **ACL Advanced Features**: Comprehensive guide covering template patterns, computed rules, and caching strategies
- **Docker Production Setup**: Complete production deployment guide with security best practices
- **API Reference**: Detailed documentation for all send handler endpoints and authentication APIs
- **System Architecture**: Visual diagrams showing component relationships and data flow

#### 📊 Visual Documentation
- **E2E Encryption Diagrams**: Step-by-step visual guide of encryption process
- **System Flow Charts**: Clear diagrams showing request processing and access control flow
- **Docker Architecture**: Visual representation of container setup and networking

