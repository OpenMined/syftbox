# SyftBox Go

## Quick Start

### Install Go (MacOS)

```
brew install go
```

### Install Cursor

To enhance your Go development experience, it's recommended to install the Cursor extension. After installing, add the following settings to your user settings JSON to ensure optimal configuration:
```json
"go.toolsManagement.autoUpdate": true,
"gopls": {
    "ui.semanticTokens": true,
}
```


### Install mkcert

```
brew install mkcert
```

### Run Tests
```
just run-tests
```

### Start MinIO
```
just run-minio
```

### Run the Server
```
just run-server -f config/dev.yaml
```

### Destroy MinIO
Deletes the data as well
```
just delete-minio
```

### SSH into MinIO
```
just ssh-minio
```
