Can't get notarization to work using either goreleaser's support (built on top
of quill) or using quill directly, or even using `xcrun notarytool`.

See `.envrc` for config values, as documented by goreleaser here:
https://goreleaser.com/customization/notarize/#cross-platform

Actual credentials are all in 1password.

```
export MACOS_SIGN_P12="..."
export MACOS_SIGN_PASSWORD="..."
export MACOS_NOTARY_ISSUER="..."
export MACOS_NOTARY_KEY_ID="..."
export MACOS_NOTARY_KEY="..."

export QUILL_SIGN_P12="..."
export QUILL_SIGN_PASSWORD="..."
export QUILL_NOTARY_ISSUER="..."
export QUILL_NOTARY_KEY_ID="..."
export QUILL_NOTARY_KEY="..."
```

## goreleaser

goreleaser config looks like

```yaml
# Adapted from official docs:
# https://goreleaser.com/customization/notarize/#cross-platform
notarize:
  macos:
    - enabled: '{{ isEnvSet "MACOS_SIGN_P12" }}'

      sign:
        certificate: "{{.Env.MACOS_SIGN_P12}}"
        password: "{{.Env.MACOS_SIGN_PASSWORD}}"
        # entitlements: ./path/to/entitlements.xml

      notarize:
        issuer_id: "{{.Env.MACOS_NOTARY_ISSUER_ID}}"
        key_id: "{{.Env.MACOS_NOTARY_KEY_ID}}"
        key: "{{.Env.MACOS_NOTARY_KEY}}"

        # this can apparently take a long time?
        wait: true
        timeout: 60m
```

Attempting a pre-release snapshot via `make release-dry-run` fails with an
authentication error for unknown reasons:

```
  [...]
  • sign & notarize macOS binaries
    • signing                                        binary=dist/ghavm_darwin_all/ghavm
    • notarizing and waiting - this might take a while binary=dist/ghavm_darwin_all/ghavm
  ⨯ release failed after 3s                          error=notarize: macos: dist/ghavm_darwin_all/ghavm: unable to start submission: http status="401 Unauthorized": body="Unauthenticated\n\nRequest ID: 4LBNFD32YYRMV2263S4YLLG2ZE.0.0\n"
```

With the exact same env vars and values (but `s/MACOS/QUILL/`), quill does seem
to authenticate and create a pending notarization request, but every request
I've made gets stuck in a pending state:

```
go run github.com/anchore/quill/cmd/quill@latest sign-and-notarize dist/ghavm_darwin_all/ghavm -vv
[0000]  INFO quill version: [not provided]
[0000] DEBUG config:
  log:
      quiet: false
      level: debug
      file: ""
  dev:
      profile: none
  path: dist/ghavm_darwin_all/ghavm
  sign:
      identity: ""
      p12: *******
      timestamp-server: http://timestamp.apple.com/ts01
      ad-hoc: false
      fail-without-full-chain: true
      password: *******
      entitlements: ""
  notary:
      issuer: a0fb8bc0-0e36-41cd-bb24-3b2182689a75
      key-id: SSNQQ9N7R2
      key: *******
  status:
      wait: true
      poll-seconds: 10
      timeout-seconds: 900
  dry-run: false
[0000] DEBUG signing cert: CN=Developer ID Application: William McCutchen (A467YY9A4P),OU=A467YY9A4P,O=William McCutchen,C=US,0.9.2342.19200300.100.1.1=#130a41343637595939413450
[...]
[0000]  INFO packaging signed binaries into single multi-arch binary arches=2 binary=dist/ghavm_darwin_all/ghavm
[0000]  INFO notarizing binary binary=dist/ghavm_darwin_all/ghavm
[0000] DEBUG loading private key for notary
[0000] DEBUG starting submission name=ghavm-95173c9d9bee5414e673a4525c00ea68d24daac2b64d039c76ea4e029a721038-97b38d92
[0010] DEBUG submission status id=c9d6b0bd-98ef-49d9-a31b-38b668436dd2 status="In Progress"
[0021] DEBUG submission status id=c9d6b0bd-98ef-49d9-a31b-38b668436dd2 status="In Progress"
[0031] DEBUG submission status id=c9d6b0bd-98ef-49d9-a31b-38b668436dd2 status="In Progress"
```

## xcrun

There's some indication that maybe a zip is required, so I tried using XCode's
built-in notarization tool with a zip file:

```
$ zip ghavm.zip dist/ghavm_darwin_all/ghavm
$ xcrun notarytool submit ghavm.zip --keychain-profile "test-profile" --wait
Conducting pre-submission checks for ghavm.zip and initiating connection to the Apple notary service...
Submission ID received
  id: 53c88210-1646-4dd2-aecb-cde49051cb9c
Upload progress: 100.00% (5.32 MB of 5.32 MB)
Successfully uploaded file
  id: 53c88210-1646-4dd2-aecb-cde49051cb9c
  path: /Users/mccutchen/workspace/ghavm/ghavm.zip
Waiting for processing to complete.
```

But nothing gets past the pending state.

## status

Both `notarytool` and  `quill` show the same pending notarization requests:

```
$ xcrun notarytool history --keychain-profile "test-profile"
Successfully received submission history.
  history
    --------------------------------------------------
    createdDate: 2025-07-19T02:11:54.006Z
    id: 53c88210-1646-4dd2-aecb-cde49051cb9c
    name: ghavm.zip
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-19T02:05:22.381Z
    id: c9d6b0bd-98ef-49d9-a31b-38b668436dd2
    name: ghavm-95173c9d9bee5414e673a4525c00ea68d24daac2b64d039c76ea4e029a721038-97b38d92
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-19T02:04:42.257Z
    id: 8c95d0f2-4390-42c0-ba8a-3e2e2d31afa6
    name: ghavm-b34930a3a63d7a425b4efcb72d1c687a50bf9870a691b66422804a0dec334f55-a6a73021
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-18T20:42:56.046Z
    id: 35172908-4b33-4429-80d6-8b3cd01b65c7
    name: ghavm.zip
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-18T18:08:19.849Z
    id: 8e51a356-9eea-4413-b9f9-93a724526ab9
    name: ghavm-2de7c12500012191a1094b3bbac722d1c99c0d1afa84528abb38849d48845386-b6cc4856
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-18T18:07:01.496Z
    id: 5f6ef67f-0e1d-4209-97a3-bc5d5b004d08
    name: ghavm-8569c17626e9f7e791db733a88b9a10322374a1a58c2c45b69a4db7a8cdf20a8-05801f40
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-18T14:55:08.997Z
    id: df6ab36c-9dd6-46f1-9bcf-f8b01d2e00a9
    name: test-binary.zip
    status: In Progress
    --------------------------------------------------
    createdDate: 2025-07-18T11:55:02.132Z
    id: f23c1139-f3ae-467e-9471-6cd1e079b64b
    name: ghavm-fa30d5c8a5e3a5d9867fd32c6ad54afbb75a40151f3ae218c2a2f8b7dafe24cc-cc4a26df
    status: In Progress
```

```
$ go run github.com/anchore/quill/cmd/quill@latest submission list
┌──────────────────────────────────────┬─────────────────────────────────────────────────────────────────────────────────┬─────────────┬──────────────────────────┐
│ ID                                   │ NAME                                                                            │ STATUS      │ CREATED                  │
├──────────────────────────────────────┼─────────────────────────────────────────────────────────────────────────────────┼─────────────┼──────────────────────────┤
│ 53c88210-1646-4dd2-aecb-cde49051cb9c │ ghavm.zip                                                                       │ In Progress │ 2025-07-19T02:11:54.006Z │
│ c9d6b0bd-98ef-49d9-a31b-38b668436dd2 │ ghavm-95173c9d9bee5414e673a4525c00ea68d24daac2b64d039c76ea4e029a721038-97b38d92 │ In Progress │ 2025-07-19T02:05:22.381Z │
│ 8c95d0f2-4390-42c0-ba8a-3e2e2d31afa6 │ ghavm-b34930a3a63d7a425b4efcb72d1c687a50bf9870a691b66422804a0dec334f55-a6a73021 │ In Progress │ 2025-07-19T02:04:42.257Z │
│ 35172908-4b33-4429-80d6-8b3cd01b65c7 │ ghavm.zip                                                                       │ In Progress │ 2025-07-18T20:42:56.046Z │
│ 8e51a356-9eea-4413-b9f9-93a724526ab9 │ ghavm-2de7c12500012191a1094b3bbac722d1c99c0d1afa84528abb38849d48845386-b6cc4856 │ In Progress │ 2025-07-18T18:08:19.849Z │
│ 5f6ef67f-0e1d-4209-97a3-bc5d5b004d08 │ ghavm-8569c17626e9f7e791db733a88b9a10322374a1a58c2c45b69a4db7a8cdf20a8-05801f40 │ In Progress │ 2025-07-18T18:07:01.496Z │
│ df6ab36c-9dd6-46f1-9bcf-f8b01d2e00a9 │ test-binary.zip                                                                 │ In Progress │ 2025-07-18T14:55:08.997Z │
│ f23c1139-f3ae-467e-9471-6cd1e079b64b │ ghavm-fa30d5c8a5e3a5d9867fd32c6ad54afbb75a40151f3ae218c2a2f8b7dafe24cc-cc4a26df │ In Progress │ 2025-07-18T11:55:02.132Z │
└──────────────────────────────────────┴─────────────────────────────────────────────────────────────────────────────────┴─────────────┴──────────────────────────┘
```

I've submitted an Apple Developer support request, we'll see what they say.
