import Foundation
import Security

public struct SecureTokenStore: Sendable {
  private let service: String
  private let account: String

  public init(service: String, account: String) {
    self.service = service
    self.account = account
  }

  public func load() -> String? {
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
      kSecReturnData as String: true,
      kSecMatchLimit as String: kSecMatchLimitOne,
    ]
    var result: CFTypeRef?
    guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
      let data = result as? Data
    else {
      return nil
    }
    return String(data: data, encoding: .utf8)
  }

  public func save(_ token: String) throws {
    guard let data = token.data(using: .utf8) else { throw SecureTokenStoreError.encoding }
    let identity: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
    ]
    let attributes: [String: Any] = [
      kSecValueData as String: data,
      kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
    ]
    let status = SecItemUpdate(identity as CFDictionary, attributes as CFDictionary)
    if status == errSecItemNotFound {
      var item = identity
      item.merge(attributes) { _, new in new }
      let addStatus = SecItemAdd(item as CFDictionary, nil)
      guard addStatus == errSecSuccess else { throw SecureTokenStoreError.keychain(addStatus) }
    } else if status != errSecSuccess {
      throw SecureTokenStoreError.keychain(status)
    }
  }

  public func delete() {
    let query: [String: Any] = [
      kSecClass as String: kSecClassGenericPassword,
      kSecAttrService as String: service,
      kSecAttrAccount as String: account,
    ]
    SecItemDelete(query as CFDictionary)
  }
}

public enum SecureTokenStoreError: Error {
  case encoding
  case keychain(OSStatus)
}
