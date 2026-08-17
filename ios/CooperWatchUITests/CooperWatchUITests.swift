import XCTest

final class CooperWatchUITests: XCTestCase {
  @MainActor
  func testColdLaunchHidesPairingUntilRestorationFinishes() throws {
    let app = XCUIApplication()
    app.launchEnvironment["COOP_UI_SPLASH"] = "1"
    app.launch()

    XCTAssertTrue(
      app.descendants(matching: .any)["coop-splash-screen"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.staticTexts["Opening your Coop…"].exists)
    XCTAssertFalse(app.textFields["Coop server"].exists)
    XCTAssertFalse(app.textFields["Pairing code"].exists)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Child cold-launch splash"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testRequiredUpdateAsksAChildForHelpAndBlocksPlayback() throws {
    let app = previewApp()
    app.launchEnvironment["COOP_UI_UPDATE_REQUIRED"] = "1"
    app.launch()

    XCTAssertTrue(
      app.descendants(matching: .any)["required-update-screen"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.buttons["Get the update"].isHittable)
    XCTAssertTrue(app.staticTexts["Fresh Coop gear!"].exists)
    XCTAssertFalse(app.tabBars.firstMatch.exists)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Child required update"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

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
    XCUIDevice.shared.orientation = .landscapeLeft
    defer { XCUIDevice.shared.orientation = .portrait }

    let app = previewApp()
    app.launch()

    let video = element(labeled: "Why Do Volcanoes Erupt?", in: app)
    XCTAssertTrue(video.waitForExistence(timeout: 5))
    video.tap()

    let player = app.descendants(matching: .any)["regular-video-player"]
    XCTAssertTrue(player.waitForExistence(timeout: 5))

    let landscape = NSPredicate { _, _ in
      app.frame.width > app.frame.height
        && abs(player.frame.width - app.frame.width) <= 1
        && abs(player.frame.height - app.frame.height) <= 1
    }
    expectation(for: landscape, evaluatedWith: app)
    waitForExpectations(timeout: 5)

    XCTAssertFalse(element(labeled: "Home", in: app).exists)
    XCTAssertFalse(app.navigationBars["Now watching"].exists)

    app.swipeLeft()
    let browseView = app.descendants(matching: .any)["landscape-browse-view"]
    XCTAssertTrue(browseView.waitForExistence(timeout: 5))
    XCTAssertTrue(app.descendants(matching: .any)["landscape-watch-next-heading"].exists)
    XCTAssertTrue(element(labeled: "Like", in: app).exists)
    XCTAssertTrue(element(labeled: "Not for me", in: app).exists)
    XCTAssertTrue(element(labeled: "Subscribed", in: app).exists)
    XCTAssertTrue(element(labeled: "Channel", in: app).exists)
    XCTAssertFalse(
      element(labeled: "Not for me", in: app).frame.intersects(
        element(labeled: "Subscribed", in: app).frame
      )
    )
    XCTAssertTrue(element(labeled: "Crash Course Kids", in: app).exists)
    browseView.swipeUp()
    XCTAssertTrue(element(labeled: "Share", in: app).waitForExistence(timeout: 3))
    XCTAssertFalse(element(labeled: "Home", in: app).exists)
    XCTAssertFalse(app.navigationBars["Now watching"].exists)
    XCTAssertLessThan(player.frame.width, app.frame.width)

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Regular video landscape browse"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testWatchPageOffersPolicySafeNextVideos() throws {
    let app = previewApp()
    app.launch()

    let currentVideo = element(labeled: "Why Do Volcanoes Erupt?", in: app)
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
  func testDiscoveryShelfRenders() throws {
    let app = previewApp()
    app.launch()

    if app.buttons["Discover"].waitForExistence(timeout: 2) {
      app.buttons["Discover"].tap()
    }

    let shelf = app.descendants(matching: .any)["discovery-shelf"]
    XCTAssertTrue(shelf.waitForExistence(timeout: 5))
    for _ in 0..<4 { app.swipeUp() }

    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Locked channel discovery"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testHomeChannelNameOpensSubscriptionPage() throws {
    let app = previewApp()
    app.launch()

    let channel = element(labeled: "Open Crash Course Kids", in: app)
    XCTAssertTrue(channel.waitForExistence(timeout: 5))
    channel.tap()

    XCTAssertTrue(app.buttons["Subscribed"].waitForExistence(timeout: 5))
    XCTAssertTrue(app.staticTexts["Crash Course Kids"].exists)
  }

  @MainActor
  func testPhoneHomeUsesThreeVerticalFeedTabs() throws {
    let app = previewApp()
    app.launch()

    let picker = app.descendants(matching: .any)["home-section-picker"]
    guard picker.waitForExistence(timeout: 2) else { throw XCTSkip("Phone layout") }
    XCTAssertTrue(app.buttons["Recommendations"].exists)
    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "Phone home recommendations"
    screenshot.lifetime = .keepAlways
    add(screenshot)

    homeSectionButton("Subscriptions", in: app).tap()
    XCTAssertTrue(element(labeled: "Why Do Volcanoes Erupt?", in: app).waitForExistence(timeout: 3))
    homeSectionButton("Discover", in: app).tap()
    XCTAssertTrue(app.descendants(matching: .any)["discovery-shelf"].waitForExistence(timeout: 3))
  }

  @MainActor
  func testChannelTabUsesSubscriptionTerminology() throws {
    let app = previewApp(tab: "channels")
    app.launch()

    XCTAssertTrue(app.tabBars.buttons["Subscriptions"].waitForExistence(timeout: 5))
    XCTAssertFalse(app.tabBars.buttons["Channels"].exists)
  }

  @MainActor
  func testChannelSearchMovesToScopedSearchTab() throws {
    let app = previewApp()
    app.launch()

    let channel = element(labeled: "Open Crash Course Kids", in: app)
    XCTAssertTrue(channel.waitForExistence(timeout: 5))
    channel.tap()

    let searchChannel = app.descendants(matching: .any)["channel-search-button"]
    XCTAssertTrue(searchChannel.waitForExistence(timeout: 5))
    searchChannel.tap()

    XCTAssertTrue(app.tabBars.buttons["Search"].isSelected)
    XCTAssertTrue(
      app.descendants(matching: .any)["channel-search-scope"].waitForExistence(timeout: 5))
    XCTAssertTrue(element(labeled: "Crash Course Kids", in: app).exists)

    let searchField = app.descendants(matching: .any)["child-search-field"]
    XCTAssertTrue(searchField.waitForExistence(timeout: 5))
    searchField.tap()
    searchField.typeText("volcanoes")
    app.keyboards.buttons["search"].tap()

    XCTAssertTrue(element(labeled: "Why Do Volcanoes Erupt?", in: app).waitForExistence(timeout: 5))
    XCTAssertFalse(element(labeled: "Meeting the Cleverest Octopus in the Ocean", in: app).exists)
  }

  @MainActor
  func testLandscapeSearchStaysBelowVisibleNavigationWhileTyping() throws {
    XCUIDevice.shared.orientation = .landscapeLeft
    defer { XCUIDevice.shared.orientation = .portrait }

    let app = previewApp(tab: "search")
    app.launch()

    let searchField = app.descendants(matching: .any)["child-search-field"]
    XCTAssertTrue(searchField.waitForExistence(timeout: 5))
    let homeTab = element(labeled: "Home", in: app)
    XCTAssertTrue(homeTab.exists)
    assertSearchField(searchField, staysInside: homeTab, in: app)

    searchField.tap()
    searchField.typeText("volcano")

    XCTAssertTrue(homeTab.exists)
    XCTAssertTrue(element(labeled: "Subscriptions", in: app).exists)
    assertSearchField(searchField, staysInside: homeTab, in: app)
  }

  @MainActor
  func testPadHomeUsesThreeHorizontalShelves() throws {
    let app = previewApp()
    app.launch()

    let recommendations = app.descendants(matching: .any)["home-recommendations-section"]
    guard recommendations.waitForExistence(timeout: 2) else { throw XCTSkip("iPad layout") }
    XCTAssertTrue(app.descendants(matching: .any)["home-subscriptions-section"].exists)
    XCTAssertTrue(app.descendants(matching: .any)["home-discover-section"].exists)
    let screenshot = XCTAttachment(screenshot: app.screenshot())
    screenshot.name = "iPad home shelves"
    screenshot.lifetime = .keepAlways
    add(screenshot)
  }

  @MainActor
  func testShortChannelLinkOpensSubscriptionPageAndStopsPlayback() throws {
    let app = previewApp(tab: "shorts")
    app.launch()

    let channel = app.descendants(matching: .any)["short-channel-link"]
    XCTAssertTrue(channel.waitForExistence(timeout: 5))
    channel.tap()

    XCTAssertTrue(app.buttons["Subscribed"].waitForExistence(timeout: 5))
    XCTAssertEqual(activePlayers(in: app), 0)
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
    let inactiveActionBars = app.descendants(matching: .any).matching(
      identifier: "short-action-bar"
    )
    XCTAssertEqual(inactiveActionBars.count, 0)

    let homeTab = element(labeled: "Home", in: app)
    XCTAssertTrue(homeTab.waitForExistence(timeout: 5))
    homeTab.tap()
    XCTAssertTrue(
      element(labeled: "Why Do Volcanoes Erupt?", in: app).waitForExistence(timeout: 5)
    )
    XCTAssertEqual(activePlayers(in: app), 0)
  }

  @MainActor
  func testShortsTabIsAbsentWhenDisabled() throws {
    let app = previewApp()
    app.launchEnvironment["COOP_UI_SHORTS_DISABLED"] = "1"
    app.launch()

    XCTAssertTrue(
      element(labeled: "Why Do Volcanoes Erupt?", in: app).waitForExistence(timeout: 5)
    )
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
  private func homeSectionButton(_ label: String, in app: XCUIApplication) -> XCUIElement {
    app.buttons
      .matching(identifier: "home-section-picker")
      .matching(NSPredicate(format: "label == %@", label))
      .firstMatch
  }

  @MainActor
  private func assertSearchField(
    _ searchField: XCUIElement,
    staysInside navigationTab: XCUIElement,
    in app: XCUIApplication
  ) {
    if navigationTab.frame.midY < app.frame.midY {
      XCTAssertGreaterThanOrEqual(searchField.frame.minY, navigationTab.frame.maxY)
    } else {
      XCTAssertLessThanOrEqual(searchField.frame.maxY, navigationTab.frame.minY)
    }
  }

  @MainActor
  private func element(labeled label: String, in app: XCUIApplication) -> XCUIElement {
    app.descendants(matching: .any)
      .matching(NSPredicate(format: "label == %@", label))
      .firstMatch
  }
}
