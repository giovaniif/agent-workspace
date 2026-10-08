#if canImport(SwiftUI)
import AgentwsKit
import SwiftUI

struct WorkspacePathField: View {
    @Bindable var input: WorkspacePathInput
    var placeholder = "Path on the server, ~/ or relative to home"
    var busy = false
    var enabled = true
    let add: () -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = Theme(scheme)
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                TextField(placeholder, text: $input.text)
                    .textFieldStyle(.roundedBorder)
                    .font(Metrics.mono)
                    .onKeyPress(.downArrow) {
                        input.moveDown()
                        return .handled
                    }
                    .onKeyPress(.upArrow) {
                        input.moveUp()
                        return .handled
                    }
                    .onKeyPress(.tab) { input.accept() ? .handled : .ignored }
                    .onKeyPress(.rightArrow) { input.suggestions.isEmpty ? .ignored : (input.accept() ? .handled : .ignored) }
                    .onSubmit { if !input.text.isEmpty { add() } }
                Button(busy ? "Adding…" : "Add", action: add)
                    .disabled(input.text.isEmpty || busy || !enabled)
            }
            .disabled(!enabled)
            let suggestions = Array(input.suggestions.prefix(8).enumerated())
            if !suggestions.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(suggestions, id: \.element.path) { index, dir in
                        Button {
                            input.accept(index)
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "folder").foregroundStyle(theme(.subtext))
                                Text(dir.name).font(Metrics.mono)
                                Spacer()
                                if !dir.mark.isEmpty {
                                    Text(dir.mark).font(.system(size: 10, weight: .semibold)).foregroundStyle(theme(dir.git == 1 ? .blue : .subtext))
                                }
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .contentShape(Rectangle())
                            .background(RoundedRectangle(cornerRadius: 4).fill(index == input.highlight ? theme(.blue).opacity(0.16) : .clear))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(4)
                .background(RoundedRectangle(cornerRadius: Metrics.corner).fill(theme(.mantle)))
                .overlay(RoundedRectangle(cornerRadius: Metrics.corner).stroke(theme(.surface)))
            }
            if !input.status.isEmpty {
                Text(input.status).font(.system(size: 11, design: .monospaced)).foregroundStyle(theme(.subtext)).lineLimit(1).truncationMode(.middle)
            }
        }
        .task(id: input.text) { await input.refresh() }
    }
}
#endif
