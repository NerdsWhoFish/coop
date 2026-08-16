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

    let homeTab = element(labeled: "Home", in: app)
    XCTAssertTrue(homeTab.waitForExistence(timeout: 5))
    homeTab.tap()
    XCTAssertTrue(app.staticTexts["Why Do Volcanoes Erupt?"].waitForExistence(timeout: 5))
    XCTAssertEqual(activePlayers(in: app), 0)
  }

  @MainActor
  func testShortsTabIsAbsentWhenDisabled() throws {
    let app = previewApp()
    app.launchEnvironment["COOP_UI_SHORTS_DISABLED"] = "1"
    app.launch()

    XCTAssertTrue(app.staticTexts["Why Do Volcanoes Erupt?"].waitForExistence(timeout: 5))
    XCTAssertFalse(element(labeled: "Shorts", in: app).exists)
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
    app.staticTexts.matching(identifier: "active-short-status").count
  }

  @MainActor
  private func element(labeled label: String, in app: XCUIApplication) -> XCUIElement {
    app.descendants(matching: .any)
      .matching(NSPredicate(format: "label == %@", label))
      .firstMatch
  }
}
