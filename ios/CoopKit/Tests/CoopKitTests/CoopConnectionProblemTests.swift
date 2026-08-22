import Foundation
import OpenAPIRuntime
import Testing

@testable import CoopKit

@Suite("Coop connection problem")
struct CoopConnectionProblemTests {
  private func transportFailure(_ code: Int) -> ClientError {
    ClientError(
      operationID: "getChildVideo",
      operationInput: "input",
      causeDescription: "Transport threw an error.",
      underlyingError: NSError(
        domain: NSURLErrorDomain,
        code: code,
        userInfo: [
          NSUnderlyingErrorKey: NSError(domain: kCFErrorDomainCFNetwork as String, code: code)
        ]
      )
    )
  }

  @Test("reads the hostname failure a dropped VPN produces")
  func classifiesUnresolvableHost() {
    #expect(CoopConnectionProblem(transportFailure(URLError.cannotFindHost.rawValue)) == .unreachable)
  }

  @Test("a dropped tunnel is retryable and safe to repeat")
  func unreachableIsRepeatable() {
    #expect(CoopConnectionProblem.unreachable.isRetryable)
    #expect(CoopConnectionProblem.unreachable.isSafeToRepeat)
  }

  @Test("a timeout may repeat a request the server already accepted")
  func timeoutIsNotSafeToRepeat() {
    let problem = CoopConnectionProblem(transportFailure(URLError.timedOut.rawValue))
    #expect(problem == .timedOut)
    #expect(problem.isRetryable)
    #expect(!problem.isSafeToRepeat)
  }

  @Test("an unreachable network is told apart from an absent one")
  func classifiesOffline() {
    let problem = CoopConnectionProblem(transportFailure(URLError.notConnectedToInternet.rawValue))
    #expect(problem == .offline)
  }

  @Test("an expired pairing is not retried")
  func classifiesInvalidSession() {
    let problem = CoopConnectionProblem(CoopAPIError.invalidSession)
    #expect(problem == .signedOut)
    #expect(!problem.isRetryable)
  }

  @Test("a refused request is not retried")
  func classifiesRejection() {
    #expect(!CoopConnectionProblem(CoopAPIError.shortsDisabled).isRetryable)
  }

  @Test("no child ever reads a CFNetwork dump")
  func childMessagesStayReadable() {
    let problems: [CoopConnectionProblem] = [
      .offline, .unreachable, .timedOut, .serverTrouble, .signedOut, .rejected
    ]
    for problem in problems {
      #expect(!problem.childMessage.contains("Error Domain"))
      #expect(!problem.childMessage.contains("NSURLError"))
      #expect(problem.childMessage.count < 90)
    }
  }
}
