import XCTest

final class CooperWatchUITests: XCTestCase {
  @MainActor
  func testShortsSwipeMovesTheOnlyActivePlayer() throws {
    let app = previewApp(tab: "shorts")
    app.launch()

    XCTAssertTrue(
      element(labeled: "Playing A volcano makes its own lightning", in: app)
        .waitForExistence(timeout: 5)
    )
    XCTAssertEqual(activePlayers(in: app), 1)

    let player = app.descendants(matching: .any)["active-short-player"]
    let actionBar = app.descendants(matching: .any)["active-short-action-bar"]
    XCTAssertTrue(actionBar.waitForExistence(timeout: 5))
    XCTAssertEqual(player.frame.width, app.frame.width, accuracy: 1)
    XCTAssertLessThanOrEqual(actionBar.frame.height, 64)
    XCTAssertEqual(player.frame.maxY, actionBar.frame.minY, accuracy: 1)

    app.swipeUp()

    XCTAssertTrue(
      element(labeled: "Playing Yes, octopuses change color while they dream", in: app)
        .waitForExistence(timeout: 5)
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
    app.descendants(matching: .any).matching(identifier: "active-short-player").count
  }

  @MainActor
  private func element(labeled label: String, in app: XCUIApplication) -> XCUIElement {
    app.descendants(matching: .any)
      .matching(NSPredicate(format: "label == %@", label))
      .firstMatch
  }
}
