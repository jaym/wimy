# Signing wimy's releases

macOS ties the Accessibility permission to Wimy.app's code signature
(bundle id + certificate). Local builds (`make mac-install`) and the
GitHub releases (`.github/workflows/release.yml`) are signed with the
same self-signed certificate, `wimy-dev`, so one grant covers every
build on every Mac.

## Once, on your Mac

1. Create the identity: `make mac-signing-identity` (asks for your
   password to trust it for code signing).
2. The first `make mac-install` asks whether codesign may use the key:
   choose **Always Allow**.

## Once, for CI

1. Export the identity: Keychain Access → login → My Certificates →
   right-click **wimy-dev** → Export "wimy-dev"… → format Personal
   Information Exchange (.p12), choose a password. (Export just this
   one: `security export -t identities` would export all of them.)
2. Create the `release` environment with yourself as required reviewer,
   so no signed build runs without your approval:

   ```sh
   gh api -X PUT repos/jaym/wimy/environments/release \
     -F "reviewers[][type]=User" -F "reviewers[][id]=$(gh api user -q .id)"
   ```

3. Store the identity as environment secrets, then delete the file:

   ```sh
   base64 -i wimy-dev.p12 | gh secret set WIMY_SIGNING_P12 --env release
   gh secret set WIMY_SIGNING_P12_PASSWORD --env release   # prompts
   rm wimy-dev.p12
   ```

## Releasing

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Approve the `release` deployment in the Actions tab. The workflow tests,
signs, and attaches `Wimy-0.1.0-arm64.zip` (+ `.sha256`) to the release.
Then point Nix at it: `nix/update-release.sh v0.1.0`, commit, and
`darwin-rebuild switch` wherever the home-manager module is enabled.

Anyone who can approve a `release` deployment can publish code that
inherits wimy's Accessibility permission on these Macs: keep the
reviewer list short.
