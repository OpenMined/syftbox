# Vanity Domains

This document describes the vanity domains feature that was implemented for SyftBox subdomain routing.

## Overview

The vanity domains feature provides default subdomains to users and allows them to configure custom domain names that map to their datasites, with the ability to specify custom paths within their datasite. This provides more security and flexibility than the current /datasites/ subfolder system.

## Key Features

- **{email-hash} Subdomains**: alice@example.com becomes https://ff8d9819fc0e12bf.syftbox.tld/
- **Custom Domains**: Configure any domain you control to point to your datasite
- **Custom Paths**: Each domain can point to different subdirectories within your datasite
- **Hash Domain Override**: Override your default hash subdomain path using `{email-hash}` or `default` keywords
- **{email-hash} Syntax**: Automatically use your hash without knowing it
- **Hot Reloading**: Changes to settings.yaml are applied automatically
- **Security**: Moving users to subdomains/domains means protecting pages from XSS and provides domain isolation
- **Index.html Auto-serving**: Directories with index.html files serve them automatically
- **Thread-Safe Operations**: All subdomain mappings are handled with thread-safe concurrent access

## How It Works

1. **{email-hash} Subdomains**: The server automatically maps ff8d9819fc0e12bf.syftbox.tld → syftbox.tld/datasites/alice@example.com/public
2. **Configuration**: Users can create a `settings.yaml` file in their datasite root directory
3. **Hot Reload**: When settings.yaml changes, mappings are automatically updated via blob change notifications
4. **Routing**: When a request comes in with a vanity domain, the `SubdomainRewrite` middleware rewrites the path and routes to the appropriate datasite
5. **Security**: Different CORS headers are sent on subdomains with strict isolation policies

## Configuration File

Create a `settings.yaml` file in your datasite root (e.g., `alice@example.com/settings.yaml`):

```yaml
# Datasite settings
domains:
  # Use {email-hash} or "default" to automatically use your hash subdomain
  # This allows you to override where your hash subdomain points
  '{email-hash}': /blog # e.g., ff8d9819fc0e12bf.syftbox.local → alice@example.com/blog/
  'default': /blog # Alternative syntax for the same behavior

  # Custom vanity domains
  test.local: /public # Points to alice@example.com/public/
  alice.syftbox.dev: /blog # Points to alice@example.com/blog/
  portfolio.alice.dev: /portfolio/2024 # Points to alice@example.com/portfolio/2024/
```

### Special Keywords

- **`{email-hash}`**: This special keyword is automatically replaced with your email's hash subdomain. This allows you to configure your hash subdomain without knowing the actual hash value.
- **`default`**: Alternative syntax for `{email-hash}` that provides the same functionality.

### Default Behavior

- If no settings.yaml exists, your hash subdomain (e.g., `ff8d9819fc0e12bf.syftbox.local`) automatically points to `/public`
- If settings.yaml exists but doesn't configure `{email-hash}` or `default`, the default mapping to `/public` remains
- Hash generation uses the first 16 characters of SHA256 hash of the lowercase email address

## Implementation Details

### Hash Generation

Email addresses are converted to subdomains using:

1. Normalize email to lowercase and trim whitespace
2. Generate SHA256 hash of the email
3. Take the first 16 hexadecimal characters as the subdomain

### Request Flow: HTTP Request to File Serving

1. **Request Arrival**: Client requests `https://blog.alice.dev/post.html`
2. **SubdomainRewrite Middleware**:
   - Extracts host and checks against `SubdomainMapping` service
   - Finds mapping: `blog.alice.dev` → `alice@example.com` + `/blog` path
   - Uses `sandboxedRewrite` function to rewrite URL path: `/post.html` → `/datasites/alice@example.com/blog/post.html`
   - Sets internal redirect header with security nonce
   - Re-enters routing engine with `e.HandleContext(c)`
3. **Routing**: Detects subdomain request via context flag and routes to Explorer handler
4. **Explorer Handler**:
   - Checks ACL permissions for the file
   - Serves the file with proper content-type
   - Applies security headers for subdomain isolation

### Special API Handling

- API requests on subdomains (paths starting with `/api/`) are detected but do NOT get path rewritten
- This allows APIs to work correctly on subdomains while maintaining the subdomain context

### New Datasite Creation Flow

1. **Client uploads first file**: `alice@example.com/public/index.html`
2. **Blob service** triggers change notification via `handleBlobChange`
3. **HandleBlobChange**:
   - Detects new datasite `alice@example.com` using `GetOwner()` function
   - Calls `ReloadVanityDomains()` to set up mappings
   - Generates hash: `ff8d9819fc0e12bf` using `EmailToSubdomainHash()`
   - Creates default mapping: `ff8d9819fc0e12bf.syftbox.net` → `/public`
4. **Immediate availability**: Subdomain works without server restart

### Settings.yaml Update Flow

1. **Client uploads** `alice@example.com/settings.yaml`:
   ```yaml
   domains:
     '{email-hash}': /blog
     portfolio.alice.dev: /portfolio/2024
   ```
2. **BlobService** detects and broadcasts new file changes to subscribed callbacks
3. **datasite.HandleBlobChange** recognizes it's a settings file (matches `SettingsFileName`)
4. **datasite.ReloadVanityDomains**:
   - Calls `ClearVanityDomains()` to remove existing vanity domains for alice@example.com
   - Re-adds default hash mapping via `addDefaultHashMapping()`
   - Parses new configuration using `ParseSettingsYAML()`
   - Validates domain ownership with `isAllowedDomain()`
   - Updates mappings in thread-safe `SubdomainMapping` service
5. **Hot reload complete**: New domains work immediately

### Security Features

- **Domain Ownership Validation**: Users cannot claim:
  - Other users' hash subdomains (detected via 16-character hex pattern matching)
  - The main domain (e.g., syftbox.local)
  - System domains (e.g., www.syftbox.local)
- **Hash Collision Prevention**: 16-character hex strings are identified as potential hash subdomains and blocked
- **Path Traversal Protection**: `sandboxedRewrite` uses `strings.Join` instead of `filepath.Join` to prevent `..` traversal attacks
- **Nonce-based Security**: Internal redirects use a random nonce to prevent malicious header injection
- **Security Headers on Subdomains**:
  - `X-Frame-Options: SAMEORIGIN` - Prevents clickjacking
  - `X-Content-Type-Options: nosniff` - Prevents MIME type sniffing
  - `X-XSS-Protection: 1; mode=block` - XSS protection
  - `Referrer-Policy: same-origin` - Privacy protection
  - `Content-Security-Policy` - Restricts resource loading
  - `Permissions-Policy` - Disables sensitive browser features
- **CORS Isolation**: Each subdomain has strict same-origin policy
- **ACL Integration**: All existing ACL rules still apply
- **Path Isolation**: Subdomains cannot access files outside their datasite

### Configuration Requirements

- Subdomain routing is only enabled if `cfg.HTTP.Domain` is configured (not empty)
- The middleware returns a no-op handler if domain is not configured
- Local development requests (127.0.0.1, localhost, 0.0.0.0) have fallback handling

# Testing Subdomain Routing

This guide shows how to test the subdomain routing feature locally using Docker.

## Prerequisites

- Docker and Docker Compose installed
- sudo access (for modifying `/etc/hosts`)

## Quick Start

1. **Start the server and client:**

   ```bash
   just run-docker-server
   just run-docker-client alice@example.com
   ```

2. **Add local domain:**

   ```bash
   echo "127.0.0.1   syftbox.local" | sudo tee -a /etc/hosts
   echo "127.0.0.1   www.syftbox.local" | sudo tee -a /etc/hosts
   echo "127.0.0.1   ff8d9819fc0e12bf.syftbox.local" | sudo tee -a /etc/hosts
   ```

3. **Access in your browser:**
   NOTE: You will need port 8080 on all local domain urls.

   - Main site: http://syftbox.local:8080
   - Alice's subdomain: http://ff8d9819fc0e12bf.syftbox.local:8080

4. **Subdomain Hashes**
   The autogenerated subdomains use the first 16 characters of SHA256 hash of the email.

```
echo -n "user@example.com" | shasum -a 256 | cut -c1-16
```

You can generate them easily using the just command:

```
just email-hash alice@example.com
Email: alice@example.com
Hash: ff8d9819fc0e12bf

URL: http://ff8d9819fc0e12bf.syftbox.local:8080/
```

Or for a live url:

```
just email-hash alice@example.com syftbox.net
Email: alice@example.com
Hash: ff8d9819fc0e12bf

URL: https://ff8d9819fc0e12bf.syftbox.net/
```

5. **Default Behavior**
   The default behavior is to map {email-hash}.server.tld to /public.
   This prevents people from accidentally revealing things they might not want to.

This default behavior can be overridden using the `settings.yaml` file.

6. **Customizing Subdomains**

By placing a `settings.yaml` file in the datasite root like so:

```
.
├── blog
│   ├── index.html
│   └── syft.pub.yaml
├── public
│   └── syft.pub.yaml
├── settings.yaml
└── syft.pub.yaml
```

You can override the default behavior.

The syntax is external_domain: /datasite/path:

```yaml
domains:
  '{email-hash}': /blog # special syntax that resolves to ff8d9819fc0e12bf.server.tld
  'default': /blog # alternative syntax for the same behavior
  ff8d9819fc0e12bf.syftbox.local: /blog # explicit hash domain (same as above)
  test.local: /blog # custom domain pointing to the syftbox server
```

The above rules change the root to /blog which will then serve up the index.html.
It will only work if the permissions allow those files to be read, hence the syft.pub.yaml in that folder.

**NOTE:**

- We currently have no way to verify the owner of the datasite also owns external domains
- This will probably require some private pairing process at some point to prevent domain hijacking
- The system uses thread-safe operations for all subdomain mapping updates

The system hot-reloads whenever this file is changed via blob change notifications.

7. **CORS Headers**
   The CORS headers are different on subdomains. We apply strict isolation policies to prevent users from sending headers or JavaScript from the primary bare .syftbox.tld domain, but allow them to use {email-hash}.syftbox.tld with proper browser isolation for each subdomain.

## Performance Notes

- **First Access**: There may be a slight delay (1-3 seconds) on first access to a new subdomain due to:
  - ACL cache warming for new paths
  - Initial path resolution and routing setup
  - Database queries for access control validation
- **Subsequent Access**: Requests to the same subdomain are typically much faster due to caching
- **Hot Reloading**: Settings changes are detected and applied within seconds via blob change notifications
- **Thread Safety**: All operations are thread-safe using RWMutex for concurrent access

## Testing with Unit Tests

The vanity domain system includes comprehensive unit tests covering:

1. **Subdomain Mapping** (`subdomain_mapping_test.go`)

   - Hash generation and mapping
   - Vanity domain CRUD operations
   - Thread safety and concurrent access

2. **Security Validation** (`datasite_test.go`)

   - Domain ownership verification
   - {email-hash} and default syntax expansion
   - Rejection of unauthorized domains

3. **Middleware Routing** (`subdomain_test.go`)

   - Subdomain detection and path rewriting
   - Root path handling
   - Context propagation and nonce security

4. **Integration Tests**
   - End-to-end routing scenarios
   - Index.html auto-serving
   - API request handling on subdomains

## Future Enhancements

1. **Domain Validation**: Verify domain ownership via DNS TXT records
2. **Enhanced Security**: Implement proper domain verification for production environments
3. **Performance Optimization**: Cache frequently accessed domain mappings
4. **Monitoring**: Add metrics for subdomain usage and performance
