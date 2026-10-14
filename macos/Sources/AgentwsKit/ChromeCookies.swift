#if canImport(CommonCrypto) && canImport(SQLite3)
import CommonCrypto
import CryptoKit
import Foundation
import SQLite3

public enum ChromeCrypto {
    public static func key(password: String) -> Data {
        var key = Data(count: kCCKeySizeAES128)
        let salt = Data("saltysalt".utf8)
        let pass = Array(password.utf8).map { CChar(bitPattern: $0) }
        _ = key.withUnsafeMutableBytes { out in
            salt.withUnsafeBytes { s in
                CCKeyDerivationPBKDF(
                    CCPBKDFAlgorithm(kCCPBKDF2), pass, pass.count,
                    s.bindMemory(to: UInt8.self).baseAddress, salt.count,
                    CCPseudoRandomAlgorithm(kCCPRFHmacAlgSHA1), 1003,
                    out.bindMemory(to: UInt8.self).baseAddress, kCCKeySizeAES128
                )
            }
        }
        return key
    }

    public static func decrypt(_ data: Data, key: Data, host: String, version: Int) throws -> String {
        guard data.count > 3, data.prefix(3) == Data("v10".utf8) else { return String(decoding: data, as: UTF8.self) }
        let body = data.dropFirst(3)
        guard body.count >= kCCBlockSizeAES128, body.count % kCCBlockSizeAES128 == 0 else { throw CookieImportError("a Chrome cookie value is cut short") }
        var out = Data(count: body.count + kCCBlockSizeAES128)
        var moved = 0
        let iv = Data(repeating: 0x20, count: kCCBlockSizeAES128)
        let status = out.withUnsafeMutableBytes { o in
            body.withUnsafeBytes { b in
                key.withUnsafeBytes { k in
                    iv.withUnsafeBytes { v in
                        CCCrypt(
                            CCOperation(kCCDecrypt), CCAlgorithm(kCCAlgorithmAES128), CCOptions(kCCOptionPKCS7Padding),
                            k.baseAddress, key.count, v.baseAddress, b.baseAddress, body.count,
                            o.baseAddress, o.count, &moved
                        )
                    }
                }
            }
        }
        guard status == kCCSuccess else { throw CookieImportError("a Chrome cookie value does not decrypt: check the Safe Storage key") }
        var plain = out.prefix(moved)
        if version >= 24, plain.count >= 32, plain.prefix(32) == Data(SHA256.hash(data: Data(host.utf8))) {
            plain = plain.dropFirst(32)
        }
        return String(decoding: plain, as: UTF8.self)
    }
}

public enum ChromeCookies {
    static let chromeEpoch = 11_644_473_600.0

    public static func read(file: URL, password: String) throws -> [CookieRecord] {
        let copy = FileManager.default.temporaryDirectory.appendingPathComponent("agentws-cookies-\(UUID().uuidString)")
        try FileManager.default.copyItem(at: file, to: copy)
        defer { try? FileManager.default.removeItem(at: copy) }
        var db: OpaquePointer?
        guard sqlite3_open_v2(copy.path, &db, SQLITE_OPEN_READONLY, nil) == SQLITE_OK else {
            sqlite3_close(db)
            throw CookieImportError("cannot open the Chrome cookie database")
        }
        defer { sqlite3_close(db) }
        let version = Int(scalar(db, "SELECT value FROM meta WHERE key = 'version'") ?? "") ?? 0
        let key = ChromeCrypto.key(password: password)
        var statement: OpaquePointer?
        let sql = "SELECT host_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly FROM cookies"
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK else { throw CookieImportError("the Chrome cookie database has an unknown layout") }
        defer { sqlite3_finalize(statement) }
        var out: [CookieRecord] = []
        while sqlite3_step(statement) == SQLITE_ROW {
            let host = text(statement, 0)
            var value = text(statement, 2)
            let length = Int(sqlite3_column_bytes(statement, 3))
            if length > 0, let blob = sqlite3_column_blob(statement, 3) {
                value = try ChromeCrypto.decrypt(Data(bytes: blob, count: length), key: key, host: host, version: version)
            }
            let micros = Double(sqlite3_column_int64(statement, 5))
            out.append(CookieRecord(
                name: text(statement, 1), value: value, domain: host, path: text(statement, 4),
                expires: micros > 0 ? Date(timeIntervalSince1970: micros / 1_000_000 - chromeEpoch) : nil,
                secure: sqlite3_column_int(statement, 6) != 0, httpOnly: sqlite3_column_int(statement, 7) != 0
            ))
        }
        return out
    }

    static func text(_ statement: OpaquePointer?, _ column: Int32) -> String {
        sqlite3_column_text(statement, column).map { String(cString: $0) } ?? ""
    }

    static func scalar(_ db: OpaquePointer?, _ sql: String) -> String? {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK else { return nil }
        defer { sqlite3_finalize(statement) }
        guard sqlite3_step(statement) == SQLITE_ROW else { return nil }
        return text(statement, 0)
    }
}
#endif
