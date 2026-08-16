# App Store release package

Cooper The Cop and Cooper Watch are separate App Store products.
Each needs its own explicit App ID, App Store Connect record, signing assets, metadata, privacy answers, TestFlight track, and review submission.

The files in `parent/en-US` and `child/en-US` are drafts for review, not authorization to publish them.
The public privacy policy, support URL, screenshots, app icons, review contact details, and App Store Connect identifiers are still required.

## Repository-owned checks

- Build each archive with Xcode 26 or newer and the iOS 26 SDK or newer.
- Confirm the generated archive contains the target's `PrivacyInfo.xcprivacy`.
- Inspect every dependency in the final archive for required-reason API declarations and valid privacy manifests.
- Use fictional family and account data in screenshots and reviewer fixtures.
- Keep parent and child build numbers independent and monotonically increasing.
- Test account deletion before submission because Apple requires account-creating apps to initiate full account deletion in the app.
- Build a signed archive with `scripts/testflight.sh parent archive` or `scripts/testflight.sh child archive`.
- Upload an approved archive with `scripts/testflight.sh parent upload` or `scripts/testflight.sh child upload` after setting the three App Store Connect API-key environment variables.
- Populate the isolated reviewer environment by following `deploy/demo/README.md`.

## Decisions and approvals outside the repository

- Decide whether Cooper Watch enters the Kids Category before the first approval because that choice is effectively permanent.
- Complete both age-rating questionnaires from actual behavior rather than targeting a rating.
- Have privacy counsel approve the privacy policy and each App Privacy answer.
- Confirm that the YouTube API and embedded-player implementation has the content authorization Apple may request under Guidelines 5.2.2 and 5.2.3.
- Audit export compliance before setting `ITSAppUsesNonExemptEncryption` in either app.
- Resolve Digital Services Act trader status and the content-rights declaration.
- Have Joey approve the final metadata, screenshots, credentials, TestFlight distribution, and submission.

The parent app should use the ordinary age-rating flow rather than the Kids Category.
If Cooper Watch does not enter the Kids Category, its public metadata must not claim that it is "For Kids" or "For Children."
