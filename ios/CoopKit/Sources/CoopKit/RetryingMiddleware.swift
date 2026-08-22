import Foundation
import HTTPTypes
import OpenAPIRuntime

/// Decides whether a failed request may be sent again, and how long to wait.
///
/// Split out of the middleware so the rules can be tested without a transport.
struct RetryPolicy: Sendable {
  var maxRetries = 3
  var baseDelayMilliseconds = 300.0
  var growth = 2.4

  func allowsRetry(
    retriesSoFar: Int,
    problem: CoopConnectionProblem,
    method: HTTPRequest.Method,
    bodyIsReplayable: Bool
  ) -> Bool {
    guard retriesSoFar < maxRetries, problem.isRetryable, bodyIsReplayable else { return false }
    return Self.isIdempotent(method) || problem.isSafeToRepeat
  }

  /// Equal jitter: half the backoff is fixed, half is spread. `loadLibrary`
  /// fires four requests at once, and a tunnel coming back up releases them
  /// together, so identical delays would only collide again.
  func delay(beforeRetry retry: Int) -> Duration {
    let scaled = baseDelayMilliseconds * pow(growth, Double(max(0, retry - 1)))
    let fixed = scaled / 2
    return .milliseconds(Int(fixed + Double.random(in: 0...fixed)))
  }

  /// HTTPTypes keeps its own `isSafe` internal, and it excludes PUT and DELETE.
  static func isIdempotent(_ method: HTTPRequest.Method) -> Bool {
    switch method {
    case .get, .head, .put, .delete, .options, .trace: true
    default: false
    }
  }
}

/// Re-sends a request that failed the way a dropped VPN tunnel fails. Child
/// devices reach Coop over a VPN home, so a blipping tunnel surfaces as a
/// hostname that will not resolve rather than as a slow request.
struct RetryingMiddleware: ClientMiddleware {
  let policy: RetryPolicy

  init(policy: RetryPolicy = RetryPolicy()) {
    self.policy = policy
  }

  func intercept(
    _ request: HTTPRequest,
    body: HTTPBody?,
    baseURL: URL,
    operationID: String,
    next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)
  ) async throws -> (HTTPResponse, HTTPBody?) {
    // A streamed body cannot be read twice, so repeating it would send a
    // truncated request. Coop's bodies are all small JSON, which replays.
    let bodyIsReplayable = body.map { $0.iterationBehavior == .multiple } ?? true
    var retries = 0

    while true {
      let outcome: Result<(HTTPResponse, HTTPBody?), any Error>
      let problem: CoopConnectionProblem

      do {
        let result = try await next(request, body, baseURL)
        guard result.0.status.code >= 500 else { return result }
        outcome = .success(result)
        problem = .serverTrouble
      } catch {
        outcome = .failure(error)
        problem = CoopConnectionProblem(error)
      }

      guard
        policy.allowsRetry(
          retriesSoFar: retries,
          problem: problem,
          method: request.method,
          bodyIsReplayable: bodyIsReplayable
        )
      else { return try outcome.get() }

      retries += 1
      try await Task.sleep(for: policy.delay(beforeRetry: retries))
    }
  }
}
