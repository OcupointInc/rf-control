%{!?app_version:%global app_version 0.1.0}
%global debug_package %{nil}

Name:           rf-control-gui
Version:        %{app_version}
Release:        1%{?dist}
Summary:        Ocupoint RF hardware control desktop application
License:        MIT
BuildArch:      x86_64

Requires:       gtk3
Requires:       webkit2gtk3

%description
Local desktop control for Ocupoint Ethernet and USB-C RF hardware. Includes
guided Barracuda bring-up, customer CW and sweep control, status, discovery,
and network configuration. No cloud service or external web server is used.

%prep

%build

%install
install -Dm0755 %{_sourcedir}/cmd/rf-control-gui/build/bin/rf-control-gui-linux-amd64 \
  %{buildroot}%{_bindir}/rf-control-gui
install -Dm0644 %{_sourcedir}/packaging/rhel/rf-control-gui.desktop \
  %{buildroot}%{_datadir}/applications/rf-control-gui.desktop
install -Dm0644 %{_sourcedir}/LICENSE \
  %{buildroot}%{_licensedir}/%{name}/LICENSE

%files
%{_bindir}/rf-control-gui
%{_datadir}/applications/rf-control-gui.desktop
%license %{_licensedir}/%{name}/LICENSE

%changelog
* Thu Aug 20 2026 Ocupoint <engineering@ocupoint.com> - 0.1.0-1
- Initial RHEL 8 desktop release
