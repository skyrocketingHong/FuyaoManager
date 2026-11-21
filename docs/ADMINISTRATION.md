English | [简体中文](./ADMINISTRATION_ZH.md)

# Accounts, operation logs and console appearance

The platform sidebar's settings group provides user management, operation logs and appearance. User management and audit reads require a full administrator session; writes also require an allowed Origin and CSRF token. Members can use their own account settings and console appearance.

## Accounts and authorization

Creating a user creates a member account and returns a single-use code valid for ten minutes. The code appears only in the current dialog and is cleared on close. Reissuing expires unused older codes. New accounts redeem into a restricted session, bind a passkey for the current domain, and then enter the console. Accounts already bound can use a newly issued code for recovery.

The Manage drawer edits username/display name, changes platform roles, enables/disables/deletes accounts, assigns individual site memberships, lists/revokes active sessions and revokes passkeys.

| Scope    | Role                  | Access                                                                          |
| -------- | --------------------- | ------------------------------------------------------------------------------- |
| Platform | Administrator         | Manage accounts, site authorization and audit records                           |
| Platform | Member                | Use site business functions explicitly granted to the account                   |
| Site     | Owner, editor, viewer | Existing per-site API rules; a platform-role change does not rewrite site roles |

Transactions protect the last active administrator and each site's last active owner. An account cannot disable or delete itself; deleted accounts cannot be restored. Disabling revokes sessions, unused codes and pending ceremonies. Re-enabling never revives old artifacts. Deletion retains history, site content and business releases. Passkey revocation also revokes the account's login artifacts, and cannot remove your last passkey for the current domain. Session metadata includes full/restricted scope, last seen, expiry and the current-session marker; validity is not presented as live connectivity.

Lifecycle and membership writes share the same transaction lock. Passkey sign-in locks and rechecks the verified credential when inserting its session, preventing late verification from recreating access after revocation. These boundaries cover ASVS 8.2.1, 8.2.2, 8.3.1, 7.4.1 and 7.4.2.

## Operation logs

Detail includes the target username, before/after platform role, account status or site role, and the exact revoked session/passkey ID. Canonical values come from the write transaction; absent historical detail is not fabricated (migration 0015).

Records start accumulating after migration 0014 is deployed; historical events are not fabricated. Filter and page by operator, operation, outcome, time range or keyword. Expanded rows show event/account/site IDs, the API template and transport peer. Forwarded headers are not trusted, so the peer may be the reverse proxy.

The log covers authentication, administrative/business mutations and denied requests. Superseded reads canceled by filter changes are not recorded as failed operations. Administrative changes and their success event commit together; other requests are recorded by HTTP outcome, with persistence failures written to the service log. Allowlisted fields exclude codes, cookies, CSRF tokens, request bodies, raw query strings and credential bytes (ASVS 16.2.1, 16.2.5, 16.3.1, 16.3.2). Account details likewise expose only key/session metadata.

The management HTTP surface provides audit reads only. PostgreSQL records follow the existing database backup process; no external centralized audit archive is configured in this iteration. Detailed build output remains on the builds page.

## Console appearance

Appearance provides light, dark and system modes, twelve accent presets, a custom color picker and reset. Changes immediately update console components and CSS variables and persist in the browser. System mode follows OS changes; other tabs synchronize. The account menu also links to appearance, and the top-bar quick toggle preserves the accent. Error/warning colors remain semantic; custom accent text is adjusted for contrast against its actual surfaces.

See the [management OpenAPI](../contracts/openapi/openapi.yaml) for the contract and [UI_REFERENCE.md](./UI_REFERENCE.md) for reused UI sources. Business publishing stays isolated: account authorization and appearance changes do not apply Caddy or switch running websites.

## Manage static outputs

A platform administrator who owns the site can remove one or more static outputs from Builds and Releases. Removing build output prevents further previews and publishing from that build. Existing published copies continue serving. Removing a historical release makes it unavailable for rollback. Build and release history remains visible and can be filtered by output availability or cleanup status.

The platform checks the whole selection before deletion. The live release, any historical release still referenced by the edge and active previews are protected. After closing the preview or synchronizing the edge, select the outputs again. An interrupted removal is marked Cleanup pending and can be retried from the original record.

## Install themes from GitHub

Set `FUYAO_ADMIN_THEME_SOURCES` in the service environment. Each entry specifies a public GitHub HTTPS repository, theme ID, branch and theme directory. FuyaoHomepage uses the following configuration.

```sh
FUYAO_ADMIN_THEME_SOURCES='[{"id":"fuyao-homepage","url":"https://github.com/skyrocketingHong/FuyaoHomepage","branch":"main","path":"site"}]'
```

After restarting the service, a platform administrator can check and install updates under Themes → GitHub sources. The platform validates the theme manifest, configuration contract and npm lockfile, downloads the checked commit, installs dependencies in a private directory and registers the version. Select the version in a site's theme settings, then use the existing build, preview and publish workflow. A version already in the catalog keeps its original source. Installation progress is held by the running service. After a restart, check the catalog and retry the installation if needed.

The service needs Git, Node.js 22 or later, npm, and access to GitHub and the public npm registry. Theme build scripts run with the service account's privileges. Configure repositories whose release process you trust.
