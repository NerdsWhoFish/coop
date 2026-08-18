#if os(iOS)
  import SwiftUI
  import VisionKit

  public struct WebDeviceLinkScanner: UIViewControllerRepresentable {
    public let onScan: (String) -> Void

    public init(onScan: @escaping (String) -> Void) {
      self.onScan = onScan
    }

    public static var isSupported: Bool {
      DataScannerViewController.isSupported && DataScannerViewController.isAvailable
    }

    public func makeCoordinator() -> Coordinator { Coordinator(onScan: onScan) }

    public func makeUIViewController(context: Context) -> DataScannerViewController {
      let scanner = DataScannerViewController(
        recognizedDataTypes: [.barcode(symbologies: [.qr])],
        qualityLevel: .balanced,
        recognizesMultipleItems: false,
        isHighFrameRateTrackingEnabled: false,
        isPinchToZoomEnabled: true,
        isGuidanceEnabled: true,
        isHighlightingEnabled: true
      )
      scanner.delegate = context.coordinator
      try? scanner.startScanning()
      return scanner
    }

    public func updateUIViewController(_ controller: DataScannerViewController, context: Context) {
      if !controller.isScanning { try? controller.startScanning() }
    }

    public static func dismantleUIViewController(
      _ controller: DataScannerViewController, coordinator: Coordinator
    ) {
      controller.stopScanning()
    }

    public final class Coordinator: NSObject, DataScannerViewControllerDelegate {
      private let onScan: (String) -> Void
      private var completed = false

      init(onScan: @escaping (String) -> Void) {
        self.onScan = onScan
      }

      public func dataScanner(
        _: DataScannerViewController,
        didAdd addedItems: [RecognizedItem],
        allItems _: [RecognizedItem]
      ) {
        guard !completed else { return }
        for item in addedItems {
          if case .barcode(let barcode) = item, let value = barcode.payloadStringValue {
            completed = true
            onScan(value)
            return
          }
        }
      }
    }
  }
#endif
