import XCTest

final class CooperTheCopUITests: XCTestCase {
  @MainActor
  func testChannelMixerReordersTheExplainablePreview() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SCREEN"] = "recommendations"
    app.launch()

    XCTAssertTrue(app.staticTexts["Shape River’s feed"].waitForExistence(timeout: 5))

    let showMore = app.buttons["Show more from Draw Every Day"]
    for _ in 0..<4 where !showMore.isHittable {
      app.swipeUp()
    }
    XCTAssertTrue(showMore.isHittable)

    showMore.tap()
    showMore.tap()

    let firstRecommendation = app.descendants(matching: .any)
      .matching(NSPredicate(format: "label BEGINSWITH %@", "Number 1, Draw a snowy owl"))
      .firstMatch
    XCTAssertTrue(firstRecommendation.waitForExistence(timeout: 5))
    XCTAssertTrue(app.staticTexts["MUCH MORE"].exists)
  }
}
