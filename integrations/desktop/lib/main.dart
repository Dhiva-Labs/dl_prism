import 'package:flutter/material.dart';

import 'screens/home_screen.dart';

const _seedColor = Color(0xFF6D5EF0);

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const PrismDesktopApp());
}

/// Root widget: Material 3 theming that follows the system light/dark mode.
class PrismDesktopApp extends StatelessWidget {
  const PrismDesktopApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'DL Prism Desktop',
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
      home: const HomeScreen(),
    );
  }
}
