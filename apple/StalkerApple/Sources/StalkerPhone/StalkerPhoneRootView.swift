import StalkerShared
import SwiftUI

public struct StalkerPhoneRootView: View {
    @StateObject private var viewModel = StatsViewModel(client: nil)
    @StateObject private var discovery = BonjourDiscovery()
    @AppStorage("manualStalkerAddress") private var manualStalkerAddress = ""
    @State private var didConnectToLocalMac = false
    private let cloud = CloudKitSyncEngine()

    public init() {}

    public var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let error = viewModel.errorMessage {
                        Label(error, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                            .font(.callout)
                    }

                    connectionControls
                    StatsDashboardView(snapshot: viewModel.snapshot, isLive: viewModel.isLive)
                }
                .frame(maxWidth: 900, alignment: .leading)
            }
                .navigationTitle("Stalker")
                .toolbar {
                    Button {
                        Task {
                            guard await connectToFirstDiscoveredService() else { return }
                            await viewModel.refresh()
                            if let snapshot = viewModel.snapshot {
                                try? await cloud.push(snapshot: snapshot)
                                WatchSnapshotBridge.shared.send(snapshot: snapshot)
                            }
                        }
                    } label: {
                        Image(systemName: "arrow.clockwise")
                    }
                }
        }
        .task {
            WatchSnapshotBridge.shared.activate()
            viewModel.showStatus("Looking for Stalker on your local network")
            discovery.start()
            if !manualStalkerAddress.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                await connectToManualAddress()
            }
            Task {
                try? await Task.sleep(for: .seconds(4))
                if discovery.services.isEmpty && viewModel.snapshot == nil {
                    viewModel.showStatus("No Stalker service found. Keep the Mac awake and on the same Wi-Fi.")
                }
            }
            #if os(macOS)
            await viewModel.refresh()
            viewModel.connectLive()
            #endif
        }
        .onChange(of: discovery.services) { _, services in
            guard let service = services.first else { return }
            Task {
                await connect(to: service)
            }
        }
    }

    private var connectionControls: some View {
        HStack(spacing: 8) {
            TextField("Mac address", text: $manualStalkerAddress)
                .textFieldStyle(RoundedBorderTextFieldStyle())

            Button {
                Task {
                    await connectToManualAddress()
                }
            } label: {
                Image(systemName: "network")
            }
            .buttonStyle(.bordered)
        }
    }

    private func connectToFirstDiscoveredService() async -> Bool {
        guard let service = discovery.services.first else {
            await connectToManualAddress()
            return viewModel.snapshot != nil
        }
        await connect(to: service)
        return true
    }

    private func connect(to service: DiscoveredStalker) async {
        guard !didConnectToLocalMac || viewModel.snapshot?.device.id != service.deviceID else { return }
        let client = StalkerAPIClient(discovered: service)
        do {
            _ = try await client.health()
        } catch {
            viewModel.showStatus("Found \(service.name), but cannot reach it on \(service.baseURL.host() ?? "the network").")
            return
        }
        didConnectToLocalMac = true
        viewModel.use(client: client)
        await viewModel.refresh()
        viewModel.connectLive()
    }

    private func connectToManualAddress() async {
        guard let url = normalizedManualURL() else {
            await viewModel.refresh()
            return
        }
        let client = StalkerAPIClient(baseURL: url)
        do {
            _ = try await client.health()
        } catch {
            viewModel.showStatus("Cannot reach Stalker at \(url.host() ?? url.absoluteString).")
            return
        }
        viewModel.use(client: client)
        await viewModel.refresh()
        if viewModel.snapshot != nil {
            viewModel.connectLive()
        }
    }

    private func normalizedManualURL() -> URL? {
        let raw = manualStalkerAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty else { return nil }
        let value = raw.contains("://") ? raw : "http://\(raw)"
        guard var components = URLComponents(string: value) else { return nil }
        if components.port == nil {
            components.port = 18081
        }
        return components.url
    }
}
