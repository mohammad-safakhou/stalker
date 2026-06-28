import Combine
import Foundation

#if os(macOS)
@MainActor
public final class StalkerProcessController: ObservableObject {
    public enum State: Equatable {
        case unknown
        case running
        case stopped
        case starting
        case failed(String)
    }

    @Published public private(set) var state: State = .unknown

    private var process: Process?
    private let client: StalkerAPIClient

    public init(client: StalkerAPIClient = StalkerAPIClient()) {
        self.client = client
    }

    public func refresh() async {
        do {
            _ = try await client.snapshot()
            state = .running
        } catch {
            if case .starting = state {
                return
            }
            state = .stopped
        }
    }

    public func start() {
        guard process == nil else { return }
        state = .starting

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        proc.arguments = ["stalker", "serve"]
        proc.standardOutput = FileHandle.nullDevice
        proc.standardError = FileHandle.nullDevice
        proc.terminationHandler = { [weak self] process in
            Task { @MainActor in
                guard let self else { return }
                self.process = nil
                if process.terminationStatus == 0 {
                    self.state = .stopped
                } else {
                    self.state = .failed("Exited with status \(process.terminationStatus)")
                }
            }
        }

        do {
            try proc.run()
            process = proc
            Task {
                try? await Task.sleep(for: .seconds(1))
                await refresh()
            }
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    public func stop() {
        if let process {
            process.terminate()
            self.process = nil
        } else {
            let proc = Process()
            proc.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
            proc.arguments = ["-x", "stalker"]
            try? proc.run()
        }
        state = .stopped
    }

    public var statusText: String {
        switch state {
        case .unknown: "Checking"
        case .running: "Running"
        case .stopped: "Stopped"
        case .starting: "Starting"
        case .failed(let message): message
        }
    }
}
#else
@MainActor
public final class StalkerProcessController: ObservableObject {
    public enum State: Equatable {
        case unknown
        case running
        case stopped
        case starting
        case failed(String)
    }

    @Published public private(set) var state: State = .stopped

    public init(client: StalkerAPIClient = StalkerAPIClient()) {}

    public func refresh() async {}

    public func start() {
        state = .failed("Stalker process control is only available on macOS")
    }

    public func stop() {
        state = .stopped
    }

    public var statusText: String {
        switch state {
        case .unknown: "Checking"
        case .running: "Running"
        case .stopped: "Unavailable"
        case .starting: "Starting"
        case .failed(let message): message
        }
    }
}
#endif
