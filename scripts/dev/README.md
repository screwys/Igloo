# Development Scripts

`scripts/dev/` contains maintainer and local-development tools. It is not the
public runtime surface for a normal container install.

Web assets are bundled by the Go asset builder:

```text
cmd/igloo-assets
```

## Common Commands

| Command | Purpose |
|---|---|
| `build.sh` | Build the Go server and web assets. |
| `build.sh restart` | Build and restart the local server. |
| `build.sh android` | Build the server, then build/install the Android APK. |
| `build.sh all` | Build, restart the server, then build/install Android. |
| `container-check.sh` | Build and check the container image. |

The supported browser userscript lives at:

```text
scripts/tampermonkey/igloo-site-sync.user.js
```

Most other files in this directory are maintainer diagnostics or repair helpers,
not first-run instructions for a normal install.
