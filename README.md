# SyftBox Go


## Permissions System

SyftBox is a file-system backed public file sharing system.
Goal is to allow users to share files and directories with other users within a set of constraints defined by the owner of the file or directory.

A permissions file `syft.pub.yaml` at a location, dictates the permissions and limitations for that location.

A `syft.pub.yaml` file has the following structure

```golang

type Rule struct {
	GlobPath string  // example **/* or *.yaml or test.json or {email}/value.json
	Access   Access // r/w/admin
	Limit    Limit // file limitations
}

type SyftPermission struct {
	Terminal bool   
	Rules    []Rule 
	path string // location where the permission is defined
}
```

If we place a couple of `syft.pub.yaml` files in a directory structure, we can visualize it as a tree.

    /path/
        syft.pub.yaml -> Rule(**/*)
        public/
            syft.pub.yaml -> Rule(**/*)
        /api_data
            app1/
                rpc/
                    syft.pub.yaml -> Rule(rpc.schema.json), Rule(**/*.response), Rule(**/*.request)
            app2/
                rpc/
                    syft.pub.yaml -> Rule(rpc.schema.json), Rule(**/*.response), Rule(**/*.request)
            app3/
                rpc/
                    syft.pub.yaml -> Rule(rpc.schema.json), Rule(**/*.response), Rule(**/*.request)

Or if we flatten the rules by path, we see something like this

    /path/**/*             -> Rule
    /path/public/**/*      -> Rule

    /path/api_data/app1/rpc/rpc.schema.json -> Rule
    /path/api_data/app1/rpc/**/*.response   -> Rule
    /path/api_data/app1/rpc/**/*.request    -> Rule

    /path/api_data/app2/rpc/rpc.schema.json -> Rule
    /path/api_data/app2/rpc/**/*.response   -> Rule
    /path/api_data/app2/rpc/**/*.request    -> Rule

    /path/api_data/app3/rpc/rpc.schema.json -> Rule
    /path/api_data/app3/rpc/**/*.response   -> Rule
    /path/api_data/app3/rpc/**/*.request    -> Rule

u
Now the goal is to model this tree in a way that we can validate if a user can perform an action on a given path.

```golang

type PermissionNode struct {
    Path string
    Rules map[string]*Rule
    Children map[string]*PermissionNode
}

type PermissionTree struct {
    Root *PermissionNode
}


func (t *PermissionTree) AddRule(path string, r *Rule) error {
    // add rule
    // consider if parent is terminal or not
}

func (t *PermissionTree) AddPermissions(p *SyftPermission) error {
    // this will flatten SyftPermission -> Rules and call AddRule(path, r)
}

```

The above will forms the logical tree, now we need to build a system that can validate if a user can perform an action on a given path.

```golang

type FileAction enum {
    Read = iota
    Write
}

type FileInfo struct {
    Path string
    IsDir bool
    IsSymlink bool
    Size int64
    ModTime time.Time
    Crc32 uint32
    Hash string
}

type PermissionsSystem struct {
    Tree *PermissionTree
}

func (p *PermissionsSystem) CanAccess(user string, info FileInfo, action FileAction) bool {
    // check if user can perform the action (r/w) on the path
    // there will be an algorithm to determine the closest matching rule amongst the rule map of leaf node
    // filepath.Match?
}

func (p *PermissionsSystem) FilterAccessible(user string, paths []string, minAction FileAction) []string {
    // filter out paths that the user does not have access to
}

```

