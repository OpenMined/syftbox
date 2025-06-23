# Vanity Domains

This document describes the vanity domains feature that was implemented for SyftBox subdomain routing.

## Overview

The vanity domains feature provides default subdomains to users and allows them to configure custom domain names that map to their datasites, with the ability to specify custom paths within their datasite. This provides more security and flexibility that the current /datasites/ subfolder system.

## Key Features

- **{email-hash} Subdomains**: alice@example.com becomes https://ff8d9819fc0e12bf.syftbox.tld/
- **Custom Domains**: Configure any domain you control to point to your datasite
- **Custom Paths**: Each domain can point to different subdirectories
- **Hash Domain Override**: Override your default hash subdomain path
- **{email-hash} Syntax**: Automatically use your hash without knowing it
- **Hot Reloading**: Changes to settings.yaml are applied automatically
- **Security**: Moving users to subdomains / domains means protecting pages from XSS
- **Index.html Auto-serving**: Directories with index.html files serve them automatically

## How It Works

- **{email-hash} Subdomains** The server automatically maps ff8d9819fc0e12bf.syftbox.tld -> syftbox.tld/datasites/alice@example.com/public
1. **Configuration**: Users can create a `settings.yaml` file in their datasite root directory
3. **Hot Reload**: When settings.yaml changes, mappings are automatically updated
4. **Routing**: When a request comes in with a vanity domain, it's routed to the appropriate datasite and path
5. **Security**: Different CORS headers are sent on subdomains

## Configuration File

Create a `settings.yaml` file in your datasite root (e.g., `alice@example.com/settings.yaml`):

```yaml
# Datasite settings
domains:
  # Use {email-hash} to automatically use your hash subdomain
  # This allows you to override where your hash subdomain points
  "{email-hash}": /blog  # e.g., ff8d9819fc0e12bf.syftbox.local → alice@example.com/blog/
  
  # Custom vanity domains
  test.local: /public              # Points to alice@example.com/public/
  alice.syftbox.dev: /blog          # Points to alice@example.com/blog/
  portfolio.alice.dev: /portfolio/2024  # Points to alice@example.com/portfolio/2024/
```

### Special Syntax

- **`{email-hash}`**: This special keyword is automatically replaced with your email's hash subdomain. This allows you to configure your hash subdomain without knowing the actual hash value.

### Default Behavior

- If no settings.yaml exists, your hash subdomain (e.g., `ff8d9819fc0e12bf.syftbox.local`) automatically points to `/public`
- If settings.yaml exists but doesn't configure `{email-hash}`, the default mapping to `/public` remains

## Implementation Details

### Request Flow: HTTP Request to File Serving

1. **Request Arrival**: Client requests `https://blog.alice.dev/post.html`
2. **Subdomain Middleware**: 
   - Extracts host and checks against subdomain mappings
   - Finds mapping: `blog.alice.dev` → `alice@example.com` + `/blog` path
   - Rewrites URL path: `/post.html` → `/datasites/alice@example.com/blog/post.html`
   - Sets context flags for downstream handlers
3. **Routing**: Detects subdomain request and routes to Explorer handler
4. **Explorer Handler**:
   - Checks ACL permissions for the file
   - Serves the file with proper content-type
   - Applies security headers for subdomain isolation

### New Datasite Creation Flow

1. **Client uploads first file**: `alice@example.com/public/index.html`
2. **Blob service** triggers change notification
3. **HandleBlobChange**:
   - Detects new datasite `alice@example.com`
   - Generates hash: `ff8d9819fc0e12bf`
   - Creates mapping: `ff8d9819fc0e12bf.syftbox.net` → `/public`
4. **Immediate availability**: Subdomain works without restart

### Settings.yaml Update Flow

1. **Client uploads** `alice@example.com/settings.yaml`:
   ```yaml
   domains:
     "{email-hash}": /blog
     portfolio.alice.dev: /portfolio/2024
   ```
2. **Blob service** detects the file change
3. **HandleBlobChange** recognizes it's a settings file
4. **ReloadVanityDomains**:
   - Clears existing vanity domains for alice@example.com
   - Parses new configuration
   - Validates domain ownership
   - Updates mappings in memory
5. **Hot reload complete**: New domains work immediately

### Security Features

- **Domain Ownership Validation**: Users cannot claim:
  - Other users' hash subdomains
  - The main domain (e.g., syftbox.local)
  - System domains (e.g., www.syftbox.local)
- **Hash Detection**: 16-character hex strings are identified as hash subdomains
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
The autogenerated subdomains are just a SHA256 of the email.
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

5. Default Behavior
The default behavior is to map {email-hash}.server.tld to /public.
This prevents people from accidentally revealing things they might not want to.

This default behavior can be overriden using the `settings.yaml` file.


6. Customizing Subdomains

By placing a `settings.yaml` file in the datasite root like so:
```
.
├── blog
│   ├── index.html
│   └── syft.pub.yaml
├── public
│   └── syft.pub.yaml
├── settings.yaml
└── syft.pub.yaml
```

You can override the default behavior.

The syntax is external:/datasite/path:
```
domains:
    "{email-hash}": /blog # this is a special sytax that resolves to ff8d9819fc0e12bf.server.tld
    ff8d9819fc0e12bf.syftbox.local: /blog # the same as above but explicit
    test.local: /blog # this is a custom domain that is pointing to the syftbox server
```

The above rules change the root to /blog which will then serve up the index.html.
It will only work if the permissions allow those files to be read, hence the syft.pub.yaml in that folder.

NOTE:
- We currently have no way to know the owner of the datasite also owns that external domain, this will probably require some  
private pairing process at some point to prevent others stealing your domains.

The system should hot-reload when ever this file is changed.

7. CORS Headers
The CORS headers are different on subdomains, we will need to do some testing to get this right but the goal is to prevent users from sending headers or js from the primary bare .syftbox.tld domain, but allowing them to use {email-hash}.syftbox.tld and allowing the browser to isolate each subdomain.


## Performance Notes

- **First Access**: There may be a slight delay (1-3 seconds) on first access to a new subdomain due to:
  - ACL cache warming for new paths
  - Initial path resolution and routing setup
  - Database queries for access control validation
- **Subsequent Access**: Requests to the same subdomain are typically much faster due to caching
- **Hot Reloading**: Settings changes are detected and applied within seconds

## Testing with Unit Tests

The vanity domain system includes comprehensive unit tests covering:

1. **Subdomain Mapping** (`subdomain_mapping_test.go`)
   - Hash generation and mapping
   - Vanity domain CRUD operations
   - Thread safety and concurrent access

2. **Security Validation** (`datasite_test.go`)
   - Domain ownership verification
   - {email-hash} syntax expansion
   - Rejection of unauthorized domains

3. **Middleware Routing** (`subdomain_test.go`)
   - Subdomain detection and path rewriting
   - Root path handling
   - Context propagation

4. **Integration Tests** (`subdomain_routing_test.go`)
   - End-to-end routing scenarios
   - Index.html auto-serving
   - Relative link generation

## Future Enhancements

1. **Domain Validation**: Verify domain ownership via DNS TXT records
