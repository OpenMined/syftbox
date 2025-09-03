## Dockerfile.client.ds

The Dockerfile.client.ds builds an image containing the SyftBox client CLI.

### Features
- **Multi-architecture support**: Supports both `linux/amd64` and `linux/arm64`
- **Security-focused**: Runs as non-root user, includes checksum verification
- **Python runtime**: Includes Python 3.11 for running SyftBox applications built in Python
- **Health checks**: Built-in health monitoring
- **Minimal size**: Uses Alpine Linux base

### Building

```bash
# Build for current architecture
docker build -f docker/Dockerfile.client.ds \
  --build-arg SYFTBOX_VERSION=v0.8.3 \
  -t syftbox-client .

# Build for specific architecture
docker build -f docker/Dockerfile.client.ds \
  --build-arg SYFTBOX_VERSION=v0.8.3 \
  --platform linux/amd64 \
  -t syftbox-client:amd64 .

# Multi-architecture build (requires Docker Buildx)
docker buildx build -f docker/Dockerfile.client.ds \
  --platform linux/amd64,linux/arm64 \
  --build-arg SYFTBOX_VERSION=v0.8.3 \
  -t syftbox-client:multi .
```
### Deployment

To deploy this docker image, a minimal interaction is required. The container should run with an infinite command (it is the default entrypoint).

```bash
docker run -d -it syftbox-client
```

Then, we suggest attaching VSCode to the container and perform the initial manual setup.

#### How to attach VSCode to the container:

1. **Install the Remote - Containers extension** in VSCode (for example, [this one](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers))
2. **Start the container:**
   ```bash
   docker run -d --name syftbox-client -it syftbox-client
   ```
3. **Attach VSCode:**
   - Open Command Palette (`Cmd/Ctrl+Shift+P`)
   - Run "Remote-Containers: Attach to Running Container"
   - Select your container (`syftbox-client`)
4. **VSCode automatically opens a new IDE window connected to the container** - You're now working inside it

Then, run the following commands in the container environment:

```bash
syftbox login
# 1. input mail
# 2. check mail
# 3. input token received

syftbox
```

From there, you can start using the container to interact with syftbox.

### Running

```bash
# Run interactively
docker run --rm -it syftbox-client syftbox --help

# Check version
docker run --rm syftbox-client syftbox --version

# Test Python environment
docker run --rm syftbox-client python --version

# Test file permissions (runs as non-root)
docker run --rm syftbox-client whoami  # Should output: devuser
```

### Testing

```bash
# Test basic functionality
docker run --rm syftbox-client syftbox --version
```