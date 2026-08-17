import XCTest

final class CooperTheCopUITests: XCTestCase {
  @MainActor
  func testRequiredUpdateIsFriendlyAndBlocksTheParentApp() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SCREEN"] = "update"
    app.launch()

    XCTAssertTrue(app.descendants(matching: .any)["required-update-screen"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.buttons["Update Coop"].isHittable)
    XCTAssertTrue(app.staticTexts["A Coop update is ready"].exists)
    XCTAssertFalse(app.tabBars.firstMatch.exists)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Parent required update"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testRequestsShowLivePlaybackAndYouTubeReviewLinks() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SCREEN"] = "requests"
    app.launch()

    XCTAssertTrue(app.staticTexts["NOW WATCHING"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.staticTexts["A Castle Made from Cardboard"].exists)
    XCTAssertTrue(app.buttons["Block video"].exists)
    XCTAssertTrue(app.descendants(matching: .any)["Build It Together"].exists)
  }

  @MainActor
  func testFamilyInstanceRowsStayCompact() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SCREEN"] = "family"
    app.launch()

    let youtubeAPI = app.staticTexts["YouTube API"]
    let replaceAPIKey = app.buttons["Replace API key"]
    XCTAssertTrue(youtubeAPI.waitForExistence(timeout: 5))
    XCTAssertTrue(replaceAPIKey.waitForExistence(timeout: 5))
    XCTAssertLessThan(replaceAPIKey.frame.minY - youtubeAPI.frame.maxY, 80)
  }

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

  @MainActor
  func testChildSettingsExposeOptInChannelDiscovery() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SCREEN"] = "child-settings"
    app.launch()

    let toggle = app.switches["Suggest new channels"]
    XCTAssertTrue(toggle.waitForExistence(timeout: 5))
    XCTAssertEqual(toggle.value as? String, "1")
    let explanation = app.staticTexts.matching(
      NSPredicate(
        format: "label BEGINSWITH %@", "New-channel suggestions are locked until approved.")
    ).firstMatch
    XCTAssertTrue(explanation.exists)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Child discovery setting"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }
}
