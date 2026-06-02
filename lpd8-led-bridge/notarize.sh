#!/bin/bash
#
# notarize.sh - sign, notarize, and zip the macOS binaries for distribution.
#
# Run AFTER ./build.sh (which produces releases/rb-lpd8-led-bridge-darwin-*).
# Requires an Apple Developer account and a "Developer ID Application" cert.
#
# One-time credential setup (creates the keychain profile used below):
#   xcrun notarytool store-credentials rb-lpd8-notary \
#       --apple-id "you@example.com" --team-id "TEAMID" \
#       --password "app-specific-password"
#   (an app-specific password is created at https://account.apple.com → Sign-In & Security)
#
# Then:
#   ./notarize.sh
#
# Config via environment:
#   SIGN_IDENTITY   "Developer ID Application: Name (TEAMID)"  (auto-detected if unset)
#   NOTARY_PROFILE  keychain profile name  (default: rb-lpd8-notary)

set -euo pipefail
trap 'echo "ERROR: notarize.sh stopped at line $LINENO" >&2' ERR

OUTPUT_DIR="releases"
NOTARY_PROFILE="${NOTARY_PROFILE:-rb-lpd8-notary}"

if [ "$(uname -s)" != "Darwin" ]; then
    echo "notarize.sh only runs on macOS." >&2
    exit 1
fi

# Auto-detect the Developer ID Application identity if not provided.
# The trailing `|| true` matters: `head` closing the pipe early can SIGPIPE the
# upstream command, which pipefail+set -e would otherwise treat as fatal here —
# silently killing the script before signing/zipping. We check for empty below.
if [ -z "${SIGN_IDENTITY:-}" ]; then
    SIGN_IDENTITY="$(security find-identity -v -p codesigning 2>/dev/null \
        | grep "Developer ID Application" | head -1 \
        | sed -E 's/.*"(.*)".*/\1/' || true)"
fi
if [ -z "${SIGN_IDENTITY:-}" ]; then
    echo "No 'Developer ID Application' identity found in the keychain." >&2
    echo "Set SIGN_IDENTITY=\"Developer ID Application: Name (TEAMID)\" and retry." >&2
    exit 1
fi
echo "Signing identity : $SIGN_IDENTITY"
echo "Notary profile   : $NOTARY_PROFILE"
echo ""

shopt -s nullglob
binaries=("$OUTPUT_DIR"/rb-lpd8-led-bridge-darwin-*)
if [ ${#binaries[@]} -eq 0 ]; then
    echo "No darwin binaries in $OUTPUT_DIR/ — run ./build.sh first." >&2
    exit 1
fi

ATTEMPTS="${ATTEMPTS:-3}"   # notary submit retries (Apple's service can time out transiently)
failed=()

for bin in "${binaries[@]}"; do
    case "$bin" in *.zip) continue ;; esac   # skip any existing zips
    echo "==> $bin"

    # Sign with the hardened runtime and a secure timestamp (required for notarization).
    echo "    signing..."
    codesign --force --timestamp --options runtime --sign "$SIGN_IDENTITY" "$bin"
    codesign --verify --strict --verbose=2 "$bin"

    # Zip the signed binary (notarytool needs a zip/dmg/pkg; this zip is also the release asset).
    zip="${bin}.zip"
    echo "    zipping -> $zip"
    rm -f "$zip"
    /usr/bin/ditto -c -k "$bin" "$zip"
    [ -f "$zip" ] || { echo "ERROR: zip was not created: $zip" >&2; exit 1; }

    # Submit and wait. Retry on transient network errors (connectTimeout etc.).
    # The `if` condition exempts this from set -e / the ERR trap, so a failed
    # attempt is handled here rather than aborting the whole run.
    ok=0
    for attempt in $(seq 1 "$ATTEMPTS"); do
        echo "    submitting to Apple (attempt $attempt/$ATTEMPTS)..."
        if xcrun notarytool submit "$zip" --keychain-profile "$NOTARY_PROFILE" --wait; then
            ok=1
            break
        fi
        if [ "$attempt" -lt "$ATTEMPTS" ]; then
            echo "    submit failed (likely a network blip) — retrying in 15s..." >&2
            sleep 15
        fi
    done

    if [ "$ok" -eq 1 ]; then
        echo "Notarized: $zip"
    else
        echo "FAILED after $ATTEMPTS attempts: $zip" >&2
        failed+=("$zip")
    fi
    echo ""
done

if [ ${#failed[@]} -gt 0 ]; then
    echo "Some submissions did not complete: ${failed[*]}" >&2
    echo "These are almost always transient — just re-run ./notarize.sh." >&2
    exit 1
fi

echo "Done. Upload the *.zip files as GitHub Release assets."
echo "Note: bare CLI binaries can't be stapled — Gatekeeper verifies the"
echo "      notarization online the first time the binary is run."
