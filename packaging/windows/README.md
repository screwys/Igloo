# Windows packaging

`IglooSetup-x64.exe` is the initial installer and manual recovery path. The
installed server updates from the application and runtime ZIP assets attached
to GitHub Releases; it does not rerun setup for automatic updates.

The guided installer offers three run modes: a `LocalService` Windows service
that starts before sign-in, current-user startup after sign-in, or manual
startup from the Igloo shortcut. Application, data, and media directories are
configured independently; their defaults are `C:\Program Files\Igloo`,
`C:\ProgramData\Igloo\data`, and `C:\ProgramData\Igloo\media`. Automatic
updates and the desktop shortcut are enabled by default.

Running setup again offers repair and uninstall. The default uninstall removes
only application files. The user may explicitly remove application data while
keeping media and configuration, or remove everything including uploaded
cookies. Destructive cleanup validates Igloo ownership markers before touching
any configured storage directory.

## Release signing

Automatic updates require an Ed25519 key pair:

- `WINDOWS_UPDATE_PUBLIC_KEY_BASE64` is a repository variable containing the
  base64-encoded 32-byte public key. It is compiled into `igloo.exe`.
- `WINDOWS_UPDATE_SIGNING_KEY_BASE64` is a repository secret containing either
  the base64-encoded 32-byte seed or 64-byte private key.

The release workflow verifies that the keys match before signing
`igloo-windows-update.json`. The installed updater rejects unsigned manifests,
invalid signatures, digest or size mismatches, paths outside the Igloo install
root, archive traversal, and version downgrades.

This application-level signature protects background updates independently of
Authenticode. Until the setup executable is Authenticode-signed, Windows can
still show an unknown-publisher warning during the initial manual install.

## Local payload proof

From PowerShell, after generating templ and static assets:

```powershell
packaging/windows/build-app.ps1 -Version 3.4.0 `
  -RuntimeRevision 2026.08.30.1 `
  -UpdatePublicKey $env:WINDOWS_UPDATE_PUBLIC_KEY_BASE64 `
  -OutputDirectory build/windows/app

packaging/windows/build-runtime.ps1 `
  -OutputDirectory build/windows/runtime
```

WiX 6 builds the per-machine MSI and the single-file Burn setup executable in
`.github/workflows/windows-release.yml`. Actual service installation, ACL,
firewall, upgrade, and rollback behavior must be verified on a clean Windows 11
VM before publishing the first installer.

`write-manifest.ps1` also accepts a runtime archive without an application
archive. This is the dependency-only release shape used when the pinned
downloader runtime changes between Igloo releases. Every manifest declares the
minimum compatible Igloo version so an older service cannot activate an
incompatible runtime pack.
