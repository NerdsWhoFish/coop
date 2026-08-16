import XCTest

final class CooperWatchUITests: XCTestCase {
  @MainActor
  func testShortsStayPortraitWhenDeviceRotates() throws {
    let app = previewApp(tab: "shorts")
    app.launch()

    let player = app.descendants(matching: .any)["active-short-player"]
    XCTAssertTrue(player.waitForExistence(timeout: 5))

    XCUIDevice.shared.orientation = .landscapeLeft
    defer { XCUIDevice.shared.orientation = .portrait }

    let portrait = NSPredicate { _, _ in app.frame.height > app.frame.width }
    expectation(for: portrait, evaluatedWith: app)
    waitForExpectations(timeout: 3)
  }

  @MainActor
  func testRegularVideoUsesTheWholeScreenInLandscape() throws {
    let app = previewApp()
    app.launch()

    let video = app.staticTexts["Why Do Volcanoes Erupt?"]
    XCTAssertTrue(video.waitForExistence(timeout: 5))
    video.tap()

    let player = app.descendants(matching: .any)["regular-video-player"]
    XCTAssertTrue(player.waitForExistence(timeout: 5))

    XCUIDevice.shared.orientation = .landscapeLeft
    defer { XCUIDevice.shared.orientation = .portrait }

    let landscape = NSPredicate { _, _ in
      app.frame.width > app.frame.height
        && abs(player.frame.width - app.frame.width) <= 1
        && abs(player.frame.height - app.frame.height) <= 1
    }
    expectation(for: landscape, evaluatedWith: app)
    waitForExpectations(timeout: 5)

    XCTAssertFalse(element(labeled: "Home", in: app).exists)
    XCTAssertFalse(app.navigationBars["Now watching"].exists)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Regular video fullscreen landscape"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testWatchPageOffersPolicySafeNextVideos() throws {
    let app = previewApp()
    app.launch()

    let currentVideo = app.staticTexts["Why Do Volcanoes Erupt?"]
    XCTAssertTrue(currentVideo.waitForExistence(timeout: 5))
    currentVideo.tap()

    let heading = app.descendants(matching: .any)["watch-next-heading"]
    for _ in 0..<4 where !heading.isHittable { app.swipeUp() }
    XCTAssertTrue(heading.isHittable)
    app.swipeUp()

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Watch next recommendations"
    screenshot.lifetime = .keepAlways
    add(screenshot)

    let nextVideo = app.descendants(matching: .any)["watch-next-octopus"]
    XCTAssertTrue(nextVideo.waitForExistence(timeout: 3))
    nextVideo.tap()

    XCTAssertTrue(
      element(labeled: "Playing Meeting the Cleverest Octopus in the Ocean", in: app)
        .waitForExistence(timeout: 5)
    )
  }

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
