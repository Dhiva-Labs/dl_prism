import 'package:flutter/material.dart';

/// A small filled circle used to indicate a boolean health/status state.
class StatusDot extends StatelessWidget {
  final bool healthy;
  final double size;

  const StatusDot({super.key, required this.healthy, this.size = 10});

  @override
  Widget build(BuildContext context) {
    final color = healthy ? Colors.green : Colors.red;
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
    );
  }
}
