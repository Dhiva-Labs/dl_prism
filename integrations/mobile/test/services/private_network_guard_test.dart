import 'package:flutter_test/flutter_test.dart';
import 'package:dlprism/services/private_network_guard.dart';

void main() {
  group('isPrivateNetworkHost', () {
    test('loopback is private', () {
      expect(isPrivateNetworkHost('127.0.0.1'), isTrue);
      expect(isPrivateNetworkHost('::1'), isTrue);
    });

    test('RFC 1918 ranges are private', () {
      expect(isPrivateNetworkHost('10.0.0.5'), isTrue);
      expect(isPrivateNetworkHost('172.16.0.1'), isTrue);
      expect(isPrivateNetworkHost('172.31.255.255'), isTrue);
      expect(isPrivateNetworkHost('192.168.1.20'), isTrue);
    });

    test('172.x outside the 16-31 second octet is not private', () {
      expect(isPrivateNetworkHost('172.15.0.1'), isFalse);
      expect(isPrivateNetworkHost('172.32.0.1'), isFalse);
    });

    test('link-local is private', () {
      expect(isPrivateNetworkHost('169.254.1.1'), isTrue);
    });

    test("Tailscale's CGNAT range 100.64.0.0/10 is private", () {
      expect(isPrivateNetworkHost('100.64.0.1'), isTrue);
      expect(isPrivateNetworkHost('100.100.100.100'), isTrue);
      expect(isPrivateNetworkHost('100.127.255.255'), isTrue);
      expect(isPrivateNetworkHost('100.63.255.255'), isFalse);
      expect(isPrivateNetworkHost('100.128.0.0'), isFalse);
    });

    test('a public IPv4 address is not private', () {
      expect(isPrivateNetworkHost('8.8.8.8'), isFalse);
      expect(isPrivateNetworkHost('1.1.1.1'), isFalse);
    });

    test('IPv6 unique-local and link-local are private', () {
      expect(isPrivateNetworkHost('fd12:3456:789a::1'), isTrue);
      expect(isPrivateNetworkHost('fe80::1'), isTrue);
    });

    test('a public IPv6 address is not private', () {
      expect(isPrivateNetworkHost('2001:4860:4860::8888'), isFalse);
    });

    test('a bare hostname (mDNS, MagicDNS, LAN DHCP name) is allowed through', () {
      expect(isPrivateNetworkHost('prism.local'), isTrue);
      expect(isPrivateNetworkHost('my-gateway'), isTrue);
      expect(isPrivateNetworkHost('gateway.tailnet-name.ts.net'), isTrue);
    });
  });

  group('validateGatewayBaseUrl', () {
    test('rejects an empty value', () {
      expect(validateGatewayBaseUrl(''), isNotNull);
      expect(validateGatewayBaseUrl(null), isNotNull);
    });

    test('rejects a value with no scheme or host', () {
      expect(validateGatewayBaseUrl('192.168.1.20:8130'), isNotNull);
      expect(validateGatewayBaseUrl('not a url'), isNotNull);
    });

    test('rejects a non-http(s) scheme', () {
      expect(validateGatewayBaseUrl('ftp://192.168.1.20'), isNotNull);
    });

    test('accepts http:// for a private LAN address', () {
      expect(validateGatewayBaseUrl('http://192.168.1.20:8130'), isNull);
    });

    test('accepts http:// for loopback (emulator/local dev)', () {
      expect(validateGatewayBaseUrl('http://127.0.0.1:8130'), isNull);
      expect(validateGatewayBaseUrl('http://10.0.2.2:8130'), isNull);
    });

    test('accepts http:// for a Tailscale CGNAT address', () {
      expect(validateGatewayBaseUrl('http://100.90.10.5:8130'), isNull);
    });

    test('rejects http:// for a public IP address', () {
      expect(validateGatewayBaseUrl('http://8.8.8.8:8130'), isNotNull);
    });

    test('accepts https:// for a public host regardless', () {
      expect(validateGatewayBaseUrl('https://prism.example.com'), isNull);
    });

    test('accepts http:// for a bare hostname (cannot classify without DNS)', () {
      expect(validateGatewayBaseUrl('http://prism.local:8130'), isNull);
    });
  });
}
