import StalkerShared
import SwiftUI

@main
struct StalkerMacApp: App {
    @StateObject private var viewModel = StatsViewModel()
    @StateObject private var process = StalkerProcessController()
    private let cloud = CloudKitSyncEngine()

    var body: some Scene {
        MenuBarExtra("Stalker", systemImage: "waveform.path.ecg") {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Label(process.statusText, systemImage: processIcon)
                    Spacer()
                    Button(processButtonTitle) {
                        if process.state == .running {
                            process.stop()
                        } else {
                            process.start()
                        }
                    }
                }
                .padding([.horizontal, .top])

                StatsDashboardView(snapshot: viewModel.snapshot, isLive: viewModel.isLive)
                    .frame(width: 360)
                HStack {
                    Button("Refresh") {
                        Task { await viewModel.refresh() }
                    }
                    Button("Sync Now") {
                        Task {
                            await viewModel.refresh()
                            if let snapshot = viewModel.snapshot {
                                try? await cloud.push(snapshot: snapshot)
                            }
                        }
                    }
                    Button("Open Dashboard") {
                        NSWorkspace.shared.open(URL(string: "http://127.0.0.1:18080/ui/")!)
                    }
                }
                .padding([.horizontal, .bottom])
            }
            .task {
                await process.refresh()
                await viewModel.refresh()
                viewModel.connectLive()
            }
        }
        .menuBarExtraStyle(.window)
    }

    private var processIcon: String {
        process.state == .running ? "checkmark.circle.fill" : "power.circle"
    }

    private var processButtonTitle: String {
        process.state == .running ? "Stop" : "Start"
    }
}
