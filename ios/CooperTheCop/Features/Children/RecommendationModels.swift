import CoopKit
import Foundation

enum RecommendationSignal: String, Equatable {
  case liked
  case disliked
  case parentMore
  case parentLess
  case rewatched
  case completed
  case channelSatisfaction
  case newSubscription
  case unwatched
  case recent

  init(_ value: Components.Schemas.RecommendationReasonKind) {
    switch value {
    case .liked: self = .liked
    case .disliked: self = .disliked
    case .parentMore: self = .parentMore
    case .parentLess: self = .parentLess
    case .rewatched: self = .rewatched
    case .completed: self = .completed
    case .channelSatisfaction: self = .channelSatisfaction
    case .newSubscription: self = .newSubscription
    case .unwatched: self = .unwatched
    case .recent: self = .recent
    }
  }
}

struct FeedRecommendation: Identifiable, Equatable {
  let id: String
  let channelID: String
  let channelTitle: String
  let title: String
  let thumbnailURL: URL?
  let reason: String
  let signal: RecommendationSignal
}

struct TunableChannel: Identifiable, Equatable {
  let id: String
  let title: String
  let thumbnailURL: URL?
}

enum ChannelPreference: Int, CaseIterable, Identifiable {
  case muchLess = -2
  case less = -1
  case balanced = 0
  case more = 1
  case muchMore = 2

  var id: Int { rawValue }

  var label: String {
    switch self {
    case .muchLess: "Much less"
    case .less: "Less"
    case .balanced: "Balanced"
    case .more: "More"
    case .muchMore: "Much more"
    }
  }
}
