import Foundation
import OpenAPIRuntime

/// What went wrong reaching Coop, in terms a screen can act on. Failures nest
/// as `ClientError` over `URLError` over CFNetwork, and `localizedDescription`
/// on that chain is the raw dump a child was being shown.
public enum CoopConnectionProblem: Sendable, Equatable {
  /// The device has no network at all.
  case offline
  /// The network is up but Coop did not answer. Usually the VPN home is down.
  case unreachable
  /// Coop was reached and took too long.
  case timedOut
  /// Coop answered with a failure of its own.
  case serverTrouble
  /// The stored credential is no longer good.
  case signedOut
  /// The request was refused, and repeating it changes nothing.
  case rejected

  public init(_ error: any Error) {
    switch Self.rootError(of: error) {
    case let apiError as CoopAPIError:
      self = apiError == .invalidSession ? .signedOut : .rejected
    case let urlError as URLError:
      self = Self.classify(urlError.code)
    case let nsError as NSError where nsError.domain == NSURLErrorDomain:
      self = Self.classify(URLError.Code(rawValue: nsError.code))
    default:
      self = .rejected
    }
  }

  /// Whether repeating the request could plausibly succeed.
  public var isRetryable: Bool {
    switch self {
    case .offline, .unreachable, .timedOut, .serverTrouble: true
    case .signedOut, .rejected: false
    }
  }

  /// Whether the failure proves the request never reached Coop, and so may be
  /// repeated even when it is not idempotent. A timeout proves nothing: Coop
  /// may have accepted it and lost only the answer.
  public var isSafeToRepeat: Bool {
    switch self {
    case .offline, .unreachable: true
    case .timedOut, .serverTrouble, .signedOut, .rejected: false
    }
  }

  /// Wording for the child app. Short, blameless, and never mentions DNS.
  public var childMessage: String {
    switch self {
    case .offline:
      "Your tablet isn't on the internet right now."
    case .unreachable:
      "Coop lives at your house and it didn't answer. It usually fixes itself."
    case .timedOut:
      "Coop is being slow right now."
    case .serverTrouble:
      "Coop is having a rough moment."
    case .signedOut:
      "This device needs to be paired again. Ask a grown-up."
    case .rejected:
      "That didn't work."
    }
  }

  public var parentMessage: String {
    switch self {
    case .offline:
      "This device is not connected to a network."
    case .unreachable:
      "Coop did not answer. Check that you are on the home network or VPN."
    case .timedOut:
      "Coop took too long to answer."
    case .serverTrouble:
      "The Coop server returned an error."
    case .signedOut:
      "Your session has expired. Sign in again."
    case .rejected:
      "Coop refused that request."
    }
  }

  /// The SF Symbol a retry surface should show.
  public var symbolName: String {
    switch self {
    case .offline, .unreachable, .timedOut: "wifi.exclamationmark"
    case .serverTrouble: "exclamationmark.triangle.fill"
    case .signedOut: "person.crop.circle.badge.exclamationmark"
    case .rejected: "questionmark.circle.fill"
    }
  }

  private static func classify(_ code: URLError.Code) -> Self {
    switch code {
    case .notConnectedToInternet, .networkConnectionLost:
      .offline
    case .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed,
      .internationalRoamingOff, .dataNotAllowed, .secureConnectionFailed:
      .unreachable
    case .timedOut:
      .timedOut
    case .badServerResponse, .cannotParseResponse, .zeroByteResource:
      .serverTrouble
    default:
      .rejected
    }
  }

  /// Only the innermost link in the wrapping chain carries a readable code.
  private static func rootError(of error: any Error) -> any Error {
    if let clientError = error as? ClientError {
      return rootError(of: clientError.underlyingError)
    }
    let nsError = error as NSError
    guard nsError.domain != NSURLErrorDomain,
      let underlying = nsError.userInfo[NSUnderlyingErrorKey] as? NSError
    else { return error }
    return rootError(of: underlying)
  }
}
