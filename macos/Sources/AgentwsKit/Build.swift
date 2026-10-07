import Foundation

public enum Build {
    public static func binary(
        environment: [String: String],
        resources: String?,
        arch: String,
        isExecutable: (String) -> Bool
    ) -> String {
        if let path = environment["AGENTWS_BINARY"], !path.isEmpty { return path }
        if let resources {
            let goArch = arch == "x86_64" ? "amd64" : arch
            let bundled = "\(resources)/bin/darwin_\(goArch)/agentws"
            if isExecutable(bundled) { return bundled }
        }
        return "agentws"
    }

    public static func read(binary: String, environment: [String: String]? = nil) throws -> String {
        let process = Process()
        let output = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = [binary, "version", "--build"]
        if let environment { process.environment = environment }
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        process.standardInput = FileHandle.nullDevice
        try process.run()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            throw AgentwsError.badReply("\(binary) version --build exited \(process.terminationStatus)")
        }
        let build = String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        guard !build.isEmpty else {
            throw AgentwsError.badReply("\(binary) version --build printed nothing")
        }
        return build
    }
}
