# R8 keep rules for the release build.
#
# Deliberately near-empty. This app has no custom native surface beyond the
# stock MainActivity (no platform channels of its own — flutter_secure_storage,
# shared_preferences, http and url_launcher are all pure-Dart-facing or ship
# their own consumer proguard rules inside their AARs), and no Dart code is
# visible to R8 at all: `flutter build apk` compiles it AOT into libapp.so,
# which R8 never opens. See secure_shell_go's proguard-rules.pro (this
# project's sibling app) for the fuller version of this reasoning, including
# why a blanket `-keep class com.google.crypto.tink.**` for
# flutter_secure_storage's encryption layer is unnecessary: Tink ships its
# own precise consumer rules inside its AAR.
-keep class dev.dhivalabs.dlprism.MainActivity { *; }
