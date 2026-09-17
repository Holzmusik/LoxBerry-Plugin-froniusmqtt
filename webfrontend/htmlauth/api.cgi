#!/usr/bin/perl
# froniusmqtt API - JSON-Action-Dispatch für die Single-Page-App (index.cgi).
# Struktur/Konventionen 1:1 vom Schwester-Plugin EaseeMQTT übernommen (siehe
# dessen api.cgi): eigenes applog() statt LoxBerry::Log, url_param()-Routing,
# JSON::PP->utf8 durchgehend, run_capture() für jeden system()-Aufruf.
use LoxBerry::System;
use LoxBerry::Web;
use CGI;
use JSON::PP;
use strict;
use warnings;

my $cgi = CGI->new;

sub run_capture {
    my (@cmd) = @_;
    my $capfile = "/tmp/froniusmqtt_run_$$.out";
    open(my $oldout, '>&', \*STDOUT) or return (system(@cmd), '');
    open(my $olderr, '>&', \*STDERR) or return (system(@cmd), '');
    open(STDOUT, '>', $capfile) or return (system(@cmd), '');
    open(STDERR, '>&', \*STDOUT);
    my $rc = system(@cmd);
    open(STDOUT, '>&', $oldout);
    open(STDERR, '>&', $olderr);
    close $oldout; close $olderr;
    my $output = '';
    if (open(my $cf, '<', $capfile)) { local $/; $output = <$cf>; close $cf; }
    unlink $capfile;
    return ($rc, $output);
}

# Wie run_capture(), sendet aber zusaetzlich $stdin_text auf STDIN des
# Kindprozesses - fuer encrypt_secret()/decrypt_secret(), damit ein Passwort
# NICHT als Kommandozeilenargument uebergeben werden muss (sonst fuer die
# Dauer des Aufrufs in der Prozessliste/ps aux sichtbar).
sub run_capture_stdin {
    my ($cmd_ref, $stdin_text) = @_;
    my @cmd = @$cmd_ref;
    my $infile = "/tmp/froniusmqtt_stdin_$$.tmp";
    open(my $ifh, '>', $infile) or return (1, '');
    chmod 0600, $infile;
    print $ifh $stdin_text;
    close $ifh;

    my $capfile = "/tmp/froniusmqtt_run_$$.out";
    open(my $oldin, '<&', \*STDIN) or do { unlink $infile; return (system(@cmd), ''); };
    open(my $oldout, '>&', \*STDOUT) or do { unlink $infile; return (system(@cmd), ''); };
    open(my $olderr, '>&', \*STDERR) or do { unlink $infile; return (system(@cmd), ''); };
    open(STDIN, '<', $infile) or do { unlink $infile; return (system(@cmd), ''); };
    open(STDOUT, '>', $capfile) or do { unlink $infile; return (system(@cmd), ''); };
    open(STDERR, '>&', \*STDOUT);
    my $rc = system(@cmd);
    open(STDIN, '<&', $oldin);
    open(STDOUT, '>&', $oldout);
    open(STDERR, '>&', $olderr);
    close $oldin; close $oldout; close $olderr;
    my $output = '';
    if (open(my $cf, '<', $capfile)) { local $/; $output = <$cf>; close $cf; }
    unlink $capfile;
    unlink $infile;
    return ($rc, $output);
}

# Eigenes, simples Datei-Logging statt LoxBerry::Log's benannter Methoden -
# siehe EaseeMQTT/api.cgi fuer die Begruendung.
sub applog {
    my ($msg) = @_;
    my @t = localtime();
    my $ts = sprintf('%04d-%02d-%02d %02d:%02d:%02d', $t[5]+1900, $t[4]+1, $t[3], $t[2], $t[1], $t[0]);
    if (open(my $fh, '>>:encoding(UTF-8)', "$lbplogdir/api.log")) {
        print $fh "[$ts] $msg\n";
        close $fh;
    }
}

my $cfgfile        = "$lbpconfigdir/config.json"; # von froniusmqtt (Go-Daemon) DIREKT gelesen
my $enginelogfile  = "$lbplogdir/froniusmqtt.log";
my $SERVICE        = 'froniusmqtt.service';
# $lbpbindir kommt dynamisch aus LoxBerry::System - NICHT hart "froniusmqtt"
# verdrahtet (Name/Folder-Kollisions-Schutz, siehe postroot.sh).
my $FRONIUSMQTT_BIN = "$lbpbindir/froniusmqtt";

print $cgi->header(-type => 'application/json', -charset => 'utf-8', 'Cache-Control' => 'no-store');

sub out { print JSON::PP->new->utf8->canonical->encode($_[0]); exit 0; }
sub err { my ($msg) = @_; applog($msg); out({ error => $msg }); }

# -- LoxBerry-Systemkonfiguration direkt lesen (general.json) ---------------
# Gleicher Pfad wie MiraiPanel/bin/bridge.js, KNXtoLOX/api.cgi UND
# EaseeMQTT/api.cgi (dort ueberall bereits funktionierend).
sub read_general_json {
    my $home = $ENV{LBHOMEDIR} || '/opt/loxberry';
    my $file = "$home/config/system/general.json";
    return {} unless -f $file;
    local $/;
    open(my $fh, '<', $file) or return {};
    my $text = <$fh>;
    close $fh;
    my $data = eval { JSON::PP->new->utf8->decode($text) };
    return $data || {};
}

sub mqtt_broker_conn {
    my $general = read_general_json();
    my $m = $general->{Mqtt} || {};
    return undef unless $m->{Brokerhost};
    return {
        host => $m->{Brokerhost},
        port => $m->{Brokerport} || 1883,
        user => $m->{Brokeruser} || '',
        pass => $m->{Brokerpass} || '',
    };
}

# -- Passwort-Verschluesselung -----------------------------------------------
# Ruft den froniusmqtt-Daemon selbst als Subprocess auf ("froniusmqtt
# encrypt/decrypt <config-datei>") statt Krypto in Perl zu duplizieren -
# gleiches Muster wie EaseeMQTT.
sub crypto_call {
    my ($subcmd, $stdin_text) = @_;
    my ($rc, $out) = run_capture_stdin([$FRONIUSMQTT_BIN, $subcmd, $cfgfile], $stdin_text);
    return undef if $rc != 0;
    $out =~ s/\r?\n\z//;
    return $out;
}

sub encrypt_secret {
    my ($plain) = @_;
    return '' unless length($plain // '');
    my $enc = crypto_call('encrypt', $plain);
    err('Verschluesselung fehlgeschlagen - ist der Dienst korrekt installiert?') unless defined($enc);
    return $enc;
}

sub decrypt_secret {
    my ($stored) = @_;
    return '' unless length($stored // '');
    my $plain = crypto_call('decrypt', $stored);
    return defined($plain) ? $plain : $stored;
}

# -- config.json laden/speichern ---------------------------------------------
my %CONFIG_DEFAULTS = (
    fronius => {
        host => '', poll_interval_seconds => 5, http_timeout_seconds => 5,
        battery_control => {
            enabled => JSON::PP::false, user => 'customer', password => '', config_path => 'auto',
        },
    },
    mqtt => {
        use_local_broker => JSON::PP::true,
        host => '', port => 1883, username => '', password => '',
        topic_prefix => 'fronius/', client_id => 'froniusmqtt',
        enabled_categories => [ 'inverter', 'storage', 'meter', 'site' ],
    },
);

sub load_config {
    my %c = %CONFIG_DEFAULTS;
    if (-f $cfgfile) {
        local $/;
        open(my $fh, '<', $cfgfile) or return \%c;
        my $data = eval { JSON::PP->new->utf8->decode(<$fh>) };
        close $fh;
        if ($data) {
            for my $key (keys %$data) { $c{$key} = $data->{$key}; }
        }
    }
    return \%c;
}

sub save_config {
    my ($body) = @_;
    my %c = (%CONFIG_DEFAULTS, %$body);
    $c{fronius} = { %{ $CONFIG_DEFAULTS{fronius} }, %{ $body->{fronius} || {} } };
    $c{fronius}{battery_control} = { %{ $CONFIG_DEFAULTS{fronius}{battery_control} }, %{ $body->{fronius}{battery_control} || {} } };
    $c{mqtt} = { %{ $CONFIG_DEFAULTS{mqtt} }, %{ $body->{mqtt} || {} } };

    # Bereits gespeicherte (verschluesselte) Werte laden - fuer "Feld leer
    # gelassen bedeutet unveraendert" bei den Passwoertern unten.
    my $existing = load_config();

    if (length($c{fronius}{battery_control}{password} // '')) {
        $c{fronius}{battery_control}{password} = encrypt_secret($c{fronius}{battery_control}{password});
    } else {
        $c{fronius}{battery_control}{password} = $existing->{fronius}{battery_control}{password} // '';
    }

    # use_local_broker=true: host/port/user/passwort IMMER frisch aus
    # LoxBerrys general.json aufloesen und in config.json (die einzige, vom
    # Go-Daemon direkt gelesene Datei) schreiben.
    if ($c{mqtt}{use_local_broker}) {
        my $broker = mqtt_broker_conn();
        if ($broker) {
            $c{mqtt}{host} = $broker->{host};
            $c{mqtt}{port} = $broker->{port} + 0;
            $c{mqtt}{username} = $broker->{user};
            $c{mqtt}{password} = encrypt_secret($broker->{pass});
        }
    } elsif (length($c{mqtt}{password} // '')) {
        $c{mqtt}{password} = encrypt_secret($c{mqtt}{password});
    } else {
        $c{mqtt}{password} = $existing->{mqtt}{password} // '';
    }

    $c{fronius}{poll_interval_seconds} = ($c{fronius}{poll_interval_seconds} // 5) + 0;
    $c{fronius}{http_timeout_seconds}  = ($c{fronius}{http_timeout_seconds} // 5) + 0;

    open(my $fh, '>', $cfgfile) or err("Konnte config.json nicht schreiben: $!");
    print $fh JSON::PP->new->utf8->pretty->canonical->encode(\%c);
    close $fh;
    return \%c;
}

sub svc_active {
    my ($name) = @_;
    my (undef, $out) = run_capture('systemctl', 'is-active', $name);
    chomp $out;
    return $out;
}

sub svc_status {
    my ($name) = @_;
    my $active = svc_active($name);
    my (undef, $pid) = run_capture('systemctl', 'show', '--property', 'MainPID', '--value', $name);
    chomp $pid;
    undef $pid if !$pid || $pid eq '0' || $active ne 'active';
    return { running => ($active eq 'active') ? JSON::PP::true : JSON::PP::false, pid => $pid, status => $active };
}

sub tail_log {
    my ($file, $n) = @_;
    return '' unless -f $file;
    open(my $fh, '<:encoding(UTF-8)', $file) or return '';
    my @lines = <$fh>;
    close $fh;
    my $start = @lines > $n ? @lines - $n : 0;
    return join('', @lines[$start .. $#lines]);
}

# -- Fronius Solar API: Verbindungstest + Geraete-Discovery (rein für die
# Web-UI, unabhängig vom laufenden Go-Daemon) --------------------------------
# curl statt LWP::UserAgent - gleiche Werkzeugwahl wie bei den Schwester-
# Plugins (dort bereits verifiziert verfügbar).
sub curl_get {
    my ($url, @extra) = @_;
    my @cmd = ('curl', '-s', '--max-time', '8', @extra, $url);
    return run_capture(@cmd);
}

sub fronius_discover {
    my ($host) = @_;
    return { ok => JSON::PP::false, error => 'Kein Host angegeben' } unless length($host // '');

    my ($vrc, $vout) = curl_get("http://$host/solar_api/GetAPIVersion.cgi");
    return { ok => JSON::PP::false, error => "Wechselrichter unter $host nicht erreichbar" } if $vrc != 0;
    my $version = eval { JSON::PP->new->utf8->decode($vout) };
    return { ok => JSON::PP::false, error => 'Antwort von GetAPIVersion.cgi nicht lesbar' } unless $version;

    my ($drc, $dout) = curl_get("http://$host/solar_api/v1/GetActiveDeviceInfo.cgi?DeviceClass=System");
    return { ok => JSON::PP::true, api_version => $version, devices => undef, devices_error => 'Geraeteliste konnte nicht abgerufen werden' } if $drc != 0;
    my $devices = eval { JSON::PP->new->utf8->decode($dout) };
    return { ok => JSON::PP::true, api_version => $version, devices => undef, devices_error => 'Antwort von GetActiveDeviceInfo.cgi nicht lesbar' } unless $devices;

    my $data = $devices->{Body}{Data} || {};
    my @inverters = map { { id => $_, %{ $data->{Inverter}{$_} } } } sort keys %{ $data->{Inverter} || {} };
    my @meters    = map { { id => $_, %{ $data->{Meter}{$_} } } }    sort keys %{ $data->{Meter}    || {} };
    my @storages  = map { { id => $_, %{ $data->{Storage}{$_} } } }  sort keys %{ $data->{Storage}  || {} };

    return {
        ok => JSON::PP::true,
        api_version => $version,
        devices => { inverters => \@inverters, meters => \@meters, storages => \@storages },
    };
}

# -- Batterie-Control: Digest-Auth-Login testen (rein lesend, GET) ----------
# ACHTUNG bewusst kein Schreibtest hier - der Testbutton in der UI darf NIE
# tatsaechlich in "timeofuse" schreiben (siehe Plan-Datei/README: das
# ueberschreibt am Geraet konfigurierte Zeitplaene). Ein GET auf den
# aufgeloesten Pfad reicht, um die Digest-Zugangsdaten zu pruefen: bei
# falschem Passwort bleibt curl bei HTTP 401 haengen, bei richtigem
# Passwort kommt (je nach Firmware) 200 ODER 405 (Methode nicht erlaubt)
# zurueck - beides bedeutet "Login akzeptiert", nur 401 bedeutet "falsch".
sub battery_resolve_path {
    my ($host, $configured_path) = @_;
    return $configured_path if $configured_path && $configured_path ne 'auto';
    my (undef, $code) = curl_get("http://$host/api/config/timeofuse", '-o', '/dev/null', '-w', '%{http_code}');
    return '/api/config/timeofuse' if $code && $code ne '404';
    return '/config/timeofuse';
}

sub battery_test {
    my ($host, $user, $password, $configured_path) = @_;
    return { ok => JSON::PP::false, error => 'Host/Benutzername/Passwort fehlt' }
        unless length($host // '') && length($user // '') && length($password // '');
    my $path = battery_resolve_path($host, $configured_path);
    my (undef, $code) = curl_get(
        "http://$host$path", '--digest', '-u', "$user:$password",
        '-o', '/dev/null', '-w', '%{http_code}',
    );
    if (!defined($code) || $code eq '') {
        return { ok => JSON::PP::false, error => "Wechselrichter unter $host nicht erreichbar", path => $path };
    }
    if ($code eq '401') {
        return { ok => JSON::PP::false, error => 'Login abgelehnt - Benutzername/Passwort pruefen', path => $path, http_code => $code };
    }
    return { ok => JSON::PP::true, path => $path, http_code => $code };
}

# -- Dispatch ------------------------------------------------------------
my $action = $cgi->url_param('action') // '';
my $method = $ENV{REQUEST_METHOD} // 'GET';

applog("Request: action=$action method=$method");

# Rekursiv Passwörter/Tokens durch "***" ersetzen, BEVOR irgendetwas geloggt
# wird (siehe EaseeMQTT/api.cgi für die Begründung).
sub redact_for_log {
    my ($data) = @_;
    if (ref($data) eq 'HASH') {
        my %out;
        for my $k (keys %$data) {
            $out{$k} = ($k =~ /pass|secret|token/i) ? '***' : redact_for_log($data->{$k});
        }
        return \%out;
    }
    if (ref($data) eq 'ARRAY') {
        return [ map { redact_for_log($_) } @$data ];
    }
    return $data;
}

sub read_json_body {
    my $raw = $cgi->param('POSTDATA');
    if (!defined($raw) || $raw eq '') {
        local $/;
        $raw = <STDIN>;
    }
    return {} unless $raw;
    my $data = eval { JSON::PP->new->utf8->decode($raw) };
    if (!$data) {
        applog("JSON-Parse-Fehler im Request-Body (" . length($raw) . " Bytes): $@");
        return {};
    }
    applog("Body: " . JSON::PP->new->utf8->canonical->encode(redact_for_log($data)));
    return $data || {};
}

if ($action eq 'config') {
    if ($method eq 'POST') {
        my $body = read_json_body();
        my $cfg = save_config($body);
        run_capture('sudo', 'systemctl', 'restart', $SERVICE);
        applog('Konfiguration gespeichert, Dienst neu gestartet');
        out({ ok => JSON::PP::true });
    } else {
        my $cfg = load_config();
        my $broker = mqtt_broker_conn();
        $cfg->{local_broker_info} = $broker ? "$broker->{host}:$broker->{port}" : '';
        # Passwoerter (verschluesselt gespeichert) NIE an den Browser
        # zurueckgeben - nur ob ueberhaupt eines gesetzt ist.
        $cfg->{mqtt}{password_set} = length($cfg->{mqtt}{password} // '') ? JSON::PP::true : JSON::PP::false;
        $cfg->{mqtt}{password}  = '';
        $cfg->{fronius}{battery_control}{password_set} = length($cfg->{fronius}{battery_control}{password} // '') ? JSON::PP::true : JSON::PP::false;
        $cfg->{fronius}{battery_control}{password} = '';
        out($cfg);
    }
}
elsif ($action eq 'fronius_test') {
    my $body = read_json_body();
    my $cfg = load_config();
    my $host = length($body->{host} // '') ? $body->{host} : $cfg->{fronius}{host};
    out(fronius_discover($host));
}
elsif ($action eq 'battery_test') {
    my $body = read_json_body();
    my $cfg = load_config();
    my $host = length($body->{host} // '') ? $body->{host} : $cfg->{fronius}{host};
    my $user = length($body->{user} // '') ? $body->{user} : $cfg->{fronius}{battery_control}{user};
    my $password = length($body->{password} // '') ? $body->{password} : decrypt_secret($cfg->{fronius}{battery_control}{password});
    my $config_path = $body->{config_path} // $cfg->{fronius}{battery_control}{config_path};
    out(battery_test($host, $user, $password, $config_path));
}
elsif ($action eq 'service_status') {
    my $s = svc_status($SERVICE);
    $s->{log} = tail_log($enginelogfile, 150);
    out($s);
}
elsif ($action eq 'service_restart') {
    applog("Neustart von $SERVICE angefordert");
    my (undef, $out) = run_capture('sudo', 'systemctl', 'restart', $SERVICE);
    out({ ok => JSON::PP::true, output => $out });
}
elsif ($action eq 'api_log') {
    out({ log => tail_log("$lbplogdir/api.log", 150) });
}
else {
    err('Unbekannte Aktion');
}
