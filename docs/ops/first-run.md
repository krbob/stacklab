# First Run After an APT Install

This runbook initializes a new package-managed Stacklab installation safely.
It applies to the Debian package layout:

- application: `/usr/lib/stacklab`;
- configuration: `/etc/stacklab/stacklab.env`;
- managed workspace: `/srv/stacklab`;
- runtime data and SQLite: `/var/lib/stacklab`.

The package may start before authentication is configured. Until the initial
password hash exists, login returns `503 auth_not_configured`; do not expose the
service beyond the local host or trusted reverse proxy during bootstrap.

## 1. Check the installed service

Confirm that the package, Docker dependency, and configuration file are present:

```bash
sudo systemctl status stacklab --no-pager
sudo systemctl status docker --no-pager
sudo stat -c '%a %U:%G %n' /etc/stacklab/stacklab.env
```

The environment file must be owned by `root:root` with mode `600`. Fix an
unexpected mode before placing any secret in it:

```bash
sudo chown root:root /etc/stacklab/stacklab.env
sudo chmod 0600 /etc/stacklab/stacklab.env
```

## 2. Choose the access path

The packaged default is:

```text
STACKLAB_HTTP_ADDR=127.0.0.1:8080
STACKLAB_COOKIE_SECURE=true
```

Keep `STACKLAB_COOKIE_SECURE=true` when Stacklab is accessed through HTTPS.
Configure the reverse proxy before the first browser login; see
[Reverse Proxy Integration](systemd.md#reverse-proxy-integration). If the proxy
forwards client or protocol headers, configure both `STACKLAB_TRUSTED_PROXIES`
and `STACKLAB_TRUSTED_PROXY_SECRET`, and make the proxy overwrite the shared
secret header rather than forwarding a client-supplied value.

For a deliberate local-only plain-HTTP bootstrap, set
`STACKLAB_COOKIE_SECURE=false`, keep Stacklab bound to loopback, and use an SSH
tunnel instead of exposing port 8080:

```bash
ssh -L 8080:127.0.0.1:8080 operator@stacklab-host
```

Then use `http://127.0.0.1:8080` from the local browser. Restore the intended
HTTPS configuration before broader use.

## 3. Initialize the operator password

Create a unique password in a password manager. Stacklab accepts 12–256 Unicode
characters. For an EnvironmentFile-safe random value, generate 48 hexadecimal
characters and copy the output into the password manager:

```bash
openssl rand -hex 24
```

Do not assign the password in shell history or pass it on a command line.

Open the root-owned environment file with `sudoedit`:

```bash
sudoedit /etc/stacklab/stacklab.env
```

Temporarily add or uncomment:

```text
STACKLAB_BOOTSTRAP_PASSWORD=<password from the password manager>
```

Apply the bootstrap value:

```bash
sudo systemctl restart stacklab
sudo systemctl is-active stacklab
curl --fail --silent --show-error http://127.0.0.1:8080/api/ready
```

If the service does not become ready, inspect only the current boot's service
logs. Do not paste the environment file into an issue or chat:

```bash
sudo journalctl -u stacklab -b --no-pager -n 100
```

## 4. Remove the bootstrap secret

The bootstrap value is used only when the authentication store is empty. Once
the first hash has been written, remove `STACKLAB_BOOTSTRAP_PASSWORD` from
`/etc/stacklab/stacklab.env` and restart so it is no longer present in the
service environment:

```bash
sudoedit /etc/stacklab/stacklab.env
sudo systemctl restart stacklab
sudo systemctl is-active stacklab
curl --fail --silent --show-error http://127.0.0.1:8080/api/ready
```

Recheck the file mode after editing:

```bash
sudo stat -c '%a %U:%G %n' /etc/stacklab/stacklab.env
```

## 5. Log in and verify the installation

Open the configured HTTPS URL, or the local tunnel URL from step 2, and log in
with the saved password. Confirm:

- the dashboard loads without `auth_not_configured`;
- Docker and Stacklab health are visible;
- the managed workspace points to `/srv/stacklab`;
- a logout followed by a new login succeeds;
- `STACKLAB_BOOTSTRAP_PASSWORD` is absent from the environment file.

Changing the password later in Settings revokes existing sessions. The removed
bootstrap value never overwrites an existing password hash.

## Recovery

If login still returns `auth_not_configured`, the initial hash was not written.
Keep the service bound to loopback, restore the temporary bootstrap line, verify
that the password meets the length limit, restart once, and inspect the journal.
Remove the line and restart again immediately after successful initialization.

Do not delete `stacklab.db` to retry bootstrap: it also contains settings, jobs,
audit data, and schedules. For an installation that already contains data, take
a verified backup before any authentication recovery work.
