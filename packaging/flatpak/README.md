# Flatpak

`com.dhivalabs.prism.desktop.yml` packages DL Prism Desktop (the engine
variant: the dashboard opens in the browser) for Flatpak on the
`org.gnome.Platform` 51 runtime, together with the `prism` gateway it
starts, so the one bundle is the whole desktop setup.

## How CI builds it

The `distros` job in `.github/workflows/builds.yml` lays out the installed
trees of the gateway (from the `prism` binary the gateway job built and ran)
and of the companion (from the engine tarball the desktop job built and
smoke-tested) with `packaging/linux-distros/make-tree.sh`. The `flatpak` job
unpacks both into `packaging/flatpak/tree/` and runs flatpak-builder on this
manifest in the `ghcr.io/flathub-infra/flatpak-github-actions:gnome-51`
container, once per architecture (x86_64 and aarch64, each on a native
runner). The result is a single-file bundle,
`dl-prism-desktop-<version>-<arch>.flatpak`, whose runtime repository is
Flathub.

The `flatpak-test` job then installs that bundle on a plain Ubuntu runner,
pulling the runtime from Flathub, and runs it (`test-bundle.sh`): ldd of
every ELF file, the bundled gateway serving on localhost, and the
repository's Xvfb smoke test of the companion.

## Installing the bundle

```sh
flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo
flatpak install --user ./dl-prism-desktop-<version>-x86_64.flatpak
flatpak run com.dhivalabs.prism.desktop
```

The gateway is inside the app: `flatpak run --command=prism
com.dhivalabs.prism.desktop version`.

Permissions: network (the gateway listens on localhost and calls cloud
providers; the companion talks to it and to Ollama), GPU, and
`~/.config/prism`, so the companion and a `prism` installed on the host share
one `prism.yaml`. Usage records go to the app's own data directory
(`~/.var/app/com.dhivalabs.prism.desktop/data/prism`). Shell commands the
workbench agent runs execute inside the sandbox, with the runtime's tools.

## Flathub

Flathub builds from source, so a submission would replace the `tree`
directory source with the repository at a release tag: the gateway built
with the Go SDK extension (vendored modules, since the build sandbox has no
network) and the companion built with Flutter from a pinned source module,
and add a `<releases>` entry to the metainfo.
