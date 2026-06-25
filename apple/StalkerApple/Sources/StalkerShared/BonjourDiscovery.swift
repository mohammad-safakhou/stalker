import Combine
@preconcurrency import Foundation

@MainActor
public final class BonjourDiscovery: NSObject, ObservableObject {
    @Published public private(set) var services: [DiscoveredStalker] = []

    private let browser = NetServiceBrowser()
    private var resolving: Set<NetService> = []

    public override init() {
        super.init()
        browser.delegate = self
    }

    public func start() {
        browser.searchForServices(ofType: "_stalker._tcp.", inDomain: "local.")
    }

    public func stop() {
        browser.stop()
        resolving.removeAll()
        services.removeAll()
    }
}

public struct DiscoveredStalker: Identifiable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var baseURL: URL
    public var deviceID: String?
}

extension BonjourDiscovery: @preconcurrency NetServiceBrowserDelegate, @preconcurrency NetServiceDelegate {
    public func netServiceBrowser(_ browser: NetServiceBrowser, didFind service: NetService, moreComing: Bool) {
        service.delegate = self
        resolving.insert(service)
        service.resolve(withTimeout: 5)
    }

    public func netServiceDidResolveAddress(_ sender: NetService) {
        resolving.remove(sender)
        guard let host = sender.hostName, sender.port > 0 else { return }
        guard let url = URL(string: "http://\(host):\(sender.port)") else { return }
        let txt = sender.txtRecordData().map(NetService.dictionary(fromTXTRecord:)) ?? [:]
        let deviceID = txt["device_id"].flatMap { String(data: $0, encoding: .utf8) }
        let discovered = DiscoveredStalker(id: sender.name + "@" + host, name: sender.name, baseURL: url, deviceID: deviceID)
        if !services.contains(discovered) {
            services.append(discovered)
        }
    }
}
