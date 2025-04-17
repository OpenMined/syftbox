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

## Sync Client

On start, sync client builds a local index of the files and directories in the sync directory.
1. On first time it will be empty


## Problems with watching files

When using fs-based watch,
* Usually there's a latency of <20ms for the event to be triggered after the file is created/modified.
* On macOS, `ulimit` maybe limited to 512 which triggers "Too Many Open Files"
* On macOS, DELETEs are reported as RENAME until you clear the trash
* The events are not guaranteed to be in order. So we need to both debounce and deduplicate the events which will increase the latency.
* On Linux, if a directory is deleted, the watch just dies for that directory, even if it is re-created later.
* On Linux, inotify is known to be unreliable + rapid changes can cause the watch to be dropped.

Since they are os-specific in nature, they are presetn in rust-based solutions too - https://docs.rs/notify/latest/notify/#known-problems

When using polling,
* No ulimit issues
* Move/Rename may or may not have previous path.
* Latency is a little higher than the polling interval
* Faster latency <1s results incurs heavy CPU usage (30% M3 Pro CPU for 100ms)
