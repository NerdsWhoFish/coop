import XCTest

final class CooperWatchUITests: XCTestCase {
  @MainActor
  func testShortsSwipeMovesTheOnlyActivePlayer() throws {
    let app = previewApp(tab: "shorts")
    app.launch()

    XCTAssertTrue(app.staticTexts["A volcano makes its own lightning"].waitForExistence(timeout: 5))
    XCTAssertEqual(activePlayers(in: app), 1)

    app.swipeUp()

    XCTAssertTrue(
      app.staticTexts["Yes, octopuses change color while they dream"].waitForExistence(timeout: 5)
    )
    XCTAssertEqual(activePlayers(in: app), 1)

    app.tabBars.buttons["Home"].tap()
    XCTAssertEqual(activePlayers(in: app), 0)
  }

  @MainActor
  func testShortsTabIsAbsentWhenDisabled() throws {
    let app = previewApp()
    app.launchEnvironment["COOP_UI_SHORTS_DISABLED"] = "1"
    app.launch()

    XCTAssertTrue(app.tabBars.buttons["Home"].waitForExistence(timeout: 5))
    XCTAssertFalse(app.tabBars.buttons["Shorts"].exists)
  }

  @MainActor
  private func previewApp(tab: String? = nil) -> XCUIApplication {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_PREVIEW"] = "1"
    if let tab {
      app.launchEnvironment["COOP_UI_TAB"] = tab
    }
    return app
  }

  @MainActor
  private func activePlayers(in app: XCUIApplication) -> Int {
    app.descendants(matching: .any).matching(identifier: "active-short-player").count
  }
}
