import SwiftUI
import FacetManifestKit

/// The renderer-neutrality proof: every row here is read straight off the
/// same `manifest.*` frames `cmd/facet/web/app.js` renders as HTML — this
/// view supplies none of the presentation text itself, only SwiftUI list
/// chrome. A service/op template that ships with `.presentation` data
/// appears here with zero app change, same claim as the PWA renderer.
struct ContentView: View {
    @EnvironmentObject var store: ManifestStore

    var body: some View {
        NavigationStack {
            List {
                Section("Me") {
                    if let me = store.me {
                        Text(me["displayName"]?.stringValue ?? "(unnamed)")
                            .font(.headline)
                        Text(me["claimed"]?.boolValue == true ? "Claimed" : "Unclaimed")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        Text("Not hydrated yet").foregroundStyle(.secondary)
                    }
                }
                manifestSection("Services", rows: store.services, title: "name", subtitle: "description")
                catalogSection()
                manifestSection("Tasks", rows: store.tasks, title: "operationType", subtitle: nil)
                manifestSection("My Instances", rows: store.instances, title: "templateName", subtitle: "status")
                outboxSection()
            }
            .navigationTitle("Facet (SwiftUI spike)")
            .toolbar {
                ToolbarItem(placement: .automatic) {
                    Label(store.connected ? "Connected" : "Disconnected",
                          systemImage: store.connected ? "wifi" : "wifi.slash")
                        .foregroundStyle(store.connected ? .green : .red)
                }
            }
            .overlay {
                if store.services.isEmpty && store.ops.isEmpty && store.me == nil {
                    VStack(spacing: 8) {
                        ProgressView()
                        Text(store.statusMessage).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func manifestSection(_ title: String, rows: [JSONValue], title titleField: String, subtitle subtitleField: String?) -> some View {
        if !rows.isEmpty {
            Section("\(title) (\(rows.count))") {
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    VStack(alignment: .leading) {
                        Text(row[titleField]?.stringValue ?? "(untitled)")
                        if let subtitleField, let subtitle = row[subtitleField]?.stringValue {
                            Text(subtitle).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    /// The write-path trigger: unlike `app.js`'s descriptor-form renderer
    /// (`facet-app-ux.md` §3.6), which resolves a `manifest.op` row's
    /// `inputSchema`/`dispatch` fields into a real form before submitting,
    /// this spike proves only that a SwiftUI view CAN drive
    /// `ManifestStore.enqueue` end-to-end — an empty-object payload, no
    /// field resolution. An op requiring payload fields will come back
    /// `rejected` over the Outbox section below rather than `confirmed`;
    /// that is still a real round trip through the same envelope path a
    /// filled-in form would use. Building the actual form renderer is
    /// named, not done, in the design's residual (§7 Fire 5).
    @ViewBuilder
    private func catalogSection() -> some View {
        if !store.ops.isEmpty {
            Section("Catalog (\(store.ops.count))") {
                ForEach(Array(store.ops.enumerated()), id: \.offset) { _, row in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(row["title"]?.stringValue ?? "(untitled)")
                            if let subtitle = row["description"]?.stringValue {
                                Text(subtitle).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        if let operationType = row["operationType"]?.stringValue {
                            Button("Enqueue") {
                                Task { await store.enqueue(operationType: operationType, payload: .object([:])) }
                            }
                            .buttonStyle(.bordered)
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func outboxSection() -> some View {
        if !store.outbox.isEmpty {
            Section("Outbox (\(store.outbox.count))") {
                ForEach(store.outbox, id: \.requestID) { entry in
                    VStack(alignment: .leading) {
                        Text(entry.operationType)
                        Text(entry.errorMessage ?? entry.state)
                            .font(.caption)
                            .foregroundStyle(entry.state == "rejected" ? .red : (entry.state == "confirmed" ? .green : .secondary))
                    }
                }
            }
        }
    }
}
