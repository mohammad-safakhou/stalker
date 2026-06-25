import StalkerShared
import SwiftUI

public struct StalkerPhoneRootView: View {
    @StateObject private var viewModel = StatsViewModel()
    @StateObject private var discovery = BonjourDiscovery()
    private let cloud = CloudKitSyncEngine()

    public init() {}

    public var body: some View {
        NavigationStack {
            StatsDashboardView(snapshot: viewModel.snapshot, isLive: viewModel.isLive)
                .navigationTitle("Stalker")
                .toolbar {
                    Button {
                        Task {
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
            discovery.start()
            await viewModel.refresh()
            viewModel.connectLive()
        }
        .onChange(of: discovery.services) { _, services in
            guard let service = services.first else { return }
            viewModel.use(client: StalkerAPIClient(discovered: service))
            Task {
                await viewModel.refresh()
                viewModel.connectLive()
            }
        }
    }
}
