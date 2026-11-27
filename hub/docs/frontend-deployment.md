# MangoHub frontend deployment

The production dashboard is the `komari-deer` static theme on `https://vps.mangoqwq.com`. The existing `komari` container on Azure HK mounts `/home/mango/komari/data` at `/app/data`. Local theme files override the embedded theme, so frontend releases do not restart the backend or touch metrics, authentication, agents or nginx.

## GitHub workflow

`.github/workflows/frontend-deploy.yml` validates relevant pull requests. A push to `main` (or manual dispatch on `main`) runs Node 22, `npm ci`, 77 Node tests, TypeScript checking, the Next static export, and Python deployment tests. Only a successful verification job can deploy. The uploaded artifact contains `dist`, `komari-theme.json`, a commit revision at `dist/frontend-release.json`, and a SHA-256 checksum. Runs are serialized and the deploy step refuses a revision that is no longer current `main`.

The production environment accepts `main` only. Its configuration is:

- `FRONTEND_DEPLOY_HOST` variable: existing HK SSH host.
- `FRONTEND_DEPLOY_KEY` secret: dedicated Ed25519 key restricted to the theme receiver.
- `FRONTEND_KNOWN_HOSTS` secret: HK Ed25519 host key obtained over the existing trusted SSH connection, with strict host verification enabled.

The key's `authorized_keys` entry uses `restrict,command="/usr/bin/python3 /usr/local/libexec/mangohub-deploy-theme.py"`; no shell, port forwarding, PTY or arbitrary remote command is available. The receiver is installed root-owned from `scripts/deploy-theme.py`. Changes to the receiver require a reviewed reinstall via the operator's existing SSH access; the deployment key cannot replace it.

## Activation and rollback

The receiver accepts only `deploy <40-character commit> <64-character checksum>` and streams a gzip tar from stdin. It validates the checksum, commit identity, theme identity, file count and compressed/expanded size, rejecting links, devices, unexpected top-level paths and traversal. A process deadline and exclusive lock bound the operation.

Each deployment is retained under `data/theme-releases/<revision>-<unique suffix>`. The active `data/theme/komari-deer` is a relative symlink so it resolves inside the container mount. The original override directory is retained as a `legacy-*` release during the first deployment. Previous hashed Next assets are copied only when absent to support browsers holding older HTML. After the atomic switch, release identity, active theme and HTML are verified through the local loopback. This host's requests to its own public Cloudflare domain receive HTTP 403, so public accessibility and interactive behavior are verified from a normal browser after activation. Any loopback verification exception restores the previous target. No release directories are automatically deleted.

`data/theme-releases/last-success.json` records the active and previous relative paths. For a manual rollback, use existing operator SSH access, validate the previous path in this record, and atomically replace the active symlink with a relative link to it. Do not delete the deployed releases or overwrite the live metrics databases. After rollback, check `/frontend-release.json`, `/api/public`, the home page and its static assets.

## Manual hotfix outside CI

A local theme fix that is not yet on `main` still has to go through the same receiver — rebuilding the hub binary is not enough, because `data/theme/komari-deer` (the release symlink) shadows the theme embedded in the binary. Note also that any later CI run on `origin/main` republishes its own build and silently replaces the running theme, so a local hotfix must be deployed as a release and then committed to `main` to survive.

Build the theme, then:

```sh
printf '{"revision":"%s"}\n' "$REV" > dist/frontend-release.json   # REV = any 40-hex id, e.g. sha1 of a label
COPYFILE_DISABLE=1 tar --no-mac-metadata -czf theme.tgz dist komari-theme.json
ssh hk "sudo env SSH_ORIGINAL_COMMAND='deploy $REV $(sha256sum theme.tgz | cut -d' ' -f1)' \
  python3 /home/mango/mangohub/hub/scripts/deploy-theme.py" < theme.tgz
```

`COPYFILE_DISABLE=1 tar --no-mac-metadata` is mandatory when packaging on macOS; otherwise AppleDouble `._*` entries are added and the receiver rejects the archive with `Unexpected archive entry`. Verify with `sudo readlink data/theme/komari-deer`, `curl 127.0.0.1:25774/frontend-release.json`, and a normal browser session against the public host.

## Validation

Local checks: `python3 -m unittest discover -s tests -p 'test_deploy_theme.py' -v`, then `npm test`, `npm run typecheck`, `npm run build` from `web/theme/komari-deer`. Browser fixture suites additionally cover day/night persistence, canvas repaint, hover pause across live refreshes, country node data, resume on leave, four screen widths, details cancellation and retry. Production verification compares `/frontend-release.json` to the GitHub run SHA and exercises the live theme switch and globe hover.
