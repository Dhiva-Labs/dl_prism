import 'package:flutter/material.dart';

/// Scrolling, monospace, auto-following view of the gateway child process
/// log ring buffer.
class LogPane extends StatefulWidget {
  final List<String> lines;
  final double height;

  const LogPane({super.key, required this.lines, this.height = 220});

  @override
  State<LogPane> createState() => _LogPaneState();
}

class _LogPaneState extends State<LogPane> {
  final ScrollController _controller = ScrollController();

  @override
  void didUpdateWidget(covariant LogPane oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.lines.length != widget.lines.length) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_controller.hasClients) return;
        _controller.jumpTo(_controller.position.maxScrollExtent);
      });
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      height: widget.height,
      width: double.infinity,
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: widget.lines.isEmpty
          ? Center(
              child: Text(
                'No output yet.',
                style: TextStyle(color: scheme.onSurfaceVariant),
              ),
            )
          : Scrollbar(
              controller: _controller,
              child: ListView.builder(
                controller: _controller,
                itemCount: widget.lines.length,
                itemBuilder: (context, index) => Text(
                  widget.lines[index],
                  style: const TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 12,
                    height: 1.3,
                  ),
                ),
              ),
            ),
    );
  }
}
