# SyftBox

SyftBox is an open-source protocol that enables developers and organizations to build, deploy, and federate privacy-preserving computations seamlessly across a network. Unlock the ability to run computations on distributed datasets without centralizing data—preserving security while gaining valuable insights.

Read the [documentation](https://www.syftbox.net) for more details.

> [!WARNING]
> This project is a rewrite of the [original Python version](https://github.com/OpenMined/syft). Consequently, the linked documentation may not fully reflect the current implementation.

## Quick Start

Using the GUI, from https://github.com/OpenMined/SyftUI/releases

On macOS and Linux.
```
curl -fsSL https://syftbox.net/install.sh | sh
```

On Windows using Powershell
```
powershell -ExecutionPolicy ByPass -c "irm https://syftbox.net/install.ps1 | iex"
```

## Contributing

### Install Go
Follow the official [Go installation guide](https://golang.org/doc/install) to set up Go on your system.

### Install Just
Just is a command runner. You can install it by following the instructions on the [Just GitHub page](https://github.com/casey/just#installation).

### Setup Toolchain
Run the following command to set up the necessary toolchain:
```
just setup-toolchain
```

### Add Go Bin to Your Path
Ensure that your Go binaries are accessible by adding them to your PATH. Run the following command:
```
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Run Tests
Verify your setup by running the tests:
```
just test
```

See the [development guide](./DEVELOPMENT.md) for more details
