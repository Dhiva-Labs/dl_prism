import 'package:flutter/material.dart';

import 'app_root.dart';

const _seedColor = Color(0xFF6D5EF0);

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const PrismMobileApp());
}

/// Root widget: Material 3 theming that follows the system light/dark mode.
class PrismMobileApp extends StatelessWidget {
  const PrismMobileApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'DL Prism',
      debugShowCheckedModeBanner: false,
      themeMode: ThemeMode.system,
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: _seedColor,
          brightness: Brightness.light,
        ),
      ),
      darkTheme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: _seedColor,
          brightness: Brightness.dark,
        ),
      ),
      home: const AppRoot(),
    );
  }
}
