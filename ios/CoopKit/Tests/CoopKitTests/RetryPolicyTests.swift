import Foundation
import HTTPTypes
import Testing

@testable import CoopKit

@Suite("Retry policy")
struct RetryPolicyTests {
  private let policy = RetryPolicy()

  @Test("repeats a fetch a dropped tunnel killed")
  func retriesUnreachableGet() {
    #expect(
      policy.allowsRetry(
        retriesSoFar: 0, problem: .unreachable, method: .get, bodyIsReplayable: true))
  }

  @Test("stops after the configured number of retries")
  func stopsAtLimit() {
    #expect(
      !policy.allowsRetry(
        retriesSoFar: policy.maxRetries, problem: .unreachable, method: .get,
        bodyIsReplayable: true))
  }

  @Test("never repeats a refusal")
  func skipsUnretryableProblems() {
    #expect(
      !policy.allowsRetry(
        retriesSoFar: 0, problem: .signedOut, method: .get, bodyIsReplayable: true))
    #expect(
      !policy.allowsRetry(
        retriesSoFar: 0, problem: .rejected, method: .get, bodyIsReplayable: true))
  }

  /// A timeout may have been accepted by Coop, so repeating the POST behind
  /// "Ask to watch" would file the request twice.
  @Test("does not repeat a POST that may already have landed")
  func guardsNonIdempotentRequests() {
    #expect(
      !policy.allowsRetry(
        retriesSoFar: 0, problem: .timedOut, method: .post, bodyIsReplayable: true))
    #expect(
      policy.allowsRetry(
        retriesSoFar: 0, problem: .unreachable, method: .post, bodyIsReplayable: true))
  }

  @Test("treats PUT and DELETE as idempotent")
  func retriesIdempotentMutations() {
    #expect(
      policy.allowsRetry(
        retriesSoFar: 0, problem: .timedOut, method: .put, bodyIsReplayable: true))
    #expect(
      policy.allowsRetry(
        retriesSoFar: 0, problem: .serverTrouble, method: .delete, bodyIsReplayable: true))
  }

  @Test("never repeats a body it cannot read twice")
  func skipsStreamedBodies() {
    #expect(
      !policy.allowsRetry(
        retriesSoFar: 0, problem: .unreachable, method: .get, bodyIsReplayable: false))
  }

  @Test("backs off further on each attempt and stays under three seconds")
  func delaysGrow() {
    let first = policy.delay(beforeRetry: 1)
    let last = policy.delay(beforeRetry: policy.maxRetries)
    #expect(first >= .milliseconds(150))
    #expect(last > first)
    #expect(last < .seconds(2))
  }
}
