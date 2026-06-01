# TunnelControl.podspec — Expo native module Pod definition.
#
# Picked up by `expo prebuild` via expo-modules-autolinking, which
# scans for `expo-module.config.json` in `modules/*` and generates
# the Pods integration.

Pod::Spec.new do |s|
  s.name           = 'TunnelControl'
  s.version        = '1.0.0'
  s.summary        = 'NETunnelProviderManager bridge for iogrid iOS VPN client'
  s.description    = 'Expo native module that lets the JS layer start/stop the VPN tunnel via NETunnelProviderManager. Pairs with the PacketTunnelProvider extension target.'
  s.author         = ''
  s.homepage       = 'https://iogrid.org'
  s.platforms      = { :ios => '16.0' }
  s.source         = { :git => '' }
  s.static_framework = true

  s.dependency 'ExpoModulesCore'

  # Swift compiler flags + version.
  s.pod_target_xcconfig = {
    'DEFINES_MODULE'       => 'YES',
    'SWIFT_COMPILATION_MODE' => 'wholemodule'
  }

  # source_files paths are RELATIVE to the podspec's directory.
  # podspec lives at ios/TunnelControl.podspec, so this glob points at
  # ios/*.{h,m,swift} from the module root.
  s.source_files = "*.{h,m,swift}"
end
