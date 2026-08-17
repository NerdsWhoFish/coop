import SwiftUI

struct VideoLoadingPlaceholder: View {
  let thumbnailURL: URL?
  let accessibilityIdentifier: String

  var body: some View {
    ZStack {
      Color.black

      AsyncImage(url: thumbnailURL) { image in
        image
          .resizable()
          .scaledToFit()
      } placeholder: {
        Color.black
      }

      Color.black.opacity(0.24)

      ProgressView()
        .controlSize(.large)
        .tint(.white)
        .padding(18)
        .background(.black.opacity(0.72), in: .circle)
    }
    .allowsHitTesting(false)
    .accessibilityElement(children: .ignore)
    .accessibilityLabel("Loading video")
    .accessibilityIdentifier(accessibilityIdentifier)
  }
}
