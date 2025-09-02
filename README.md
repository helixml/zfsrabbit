# ZFSRabbit 🐰
## ZFS Snapshot Replication & Cross-Dataset Restore Server with Monitoring & Alerts

A comprehensive ZFS backup solution written in Go that provides automated snapshot management, cross-dataset restoration, remote replication via SSH/mbuffer, system health monitoring, and multi-channel alerting through email and Slack integration.

## Features

### Core Backup & Replication
- **Automated Snapshots**: Configurable cron-based snapshot creation and cleanup
- **Remote Replication**: ZFS send/receive over SSH with mbuffer for high-performance transfers
- **Incremental Backups**: Automatically detects and uses incremental snapshots to minimize transfer time
- **Cross-Dataset Restore**: Restore from any dataset on the remote server, not just your own
- **Multi-Instance Support**: Multiple ZFSRabbit servers can backup to the same remote server

### Monitoring & Health
- **ZFS Pool Monitoring**: Real-time pool health, scrub status, and error detection
- **SMART Disk Monitoring**: Temperature monitoring, reallocated sectors, pending sectors
- **System Health Dashboard**: Web-based overview of all monitored components
- **Proactive Alerting**: Configurable alert cooldowns and thresholds

### User Interfaces
- **Web Interface**: Full-featured web UI for monitoring, manual operations, and restore management
- **Slack Integration**: Complete slash command interface (`/zfsrabbit status`, `/zfsrabbit snapshot`, etc.)
- **REST API**: Programmatic access to all functionality
- **Job Tracking**: Real-time restore job progress with status updates

### Enterprise Features
- **Multi-Channel Alerts**: Email (SMTP/TLS) and Slack webhook notifications
- **Remote Dataset Discovery**: Browse and restore from all datasets on remote server
- **Restore Job Management**: Track multiple concurrent restore operations
- **Comprehensive Test Suite**: Full unit test coverage for reliable deployment
- **Systemd Integration**: Production-ready service with proper logging

## Requirements

- Linux system with ZFS utilities installed
- `smartctl` for disk monitoring (traditional drives)
- `nvme-cli` for NVMe SSD monitoring (recommended for NVMe drives)
- `mbuffer` for efficient data transfer
- SSH access to remote backup server
- Go 1.24+ for building

## Installation

1. **Build the binary:**
   ```bash
   go build -o zfsrabbit
   sudo cp zfsrabbit /usr/local/bin/
   ```

2. **Create configuration directory:**
   ```bash
   sudo mkdir -p /etc/zfsrabbit
   sudo cp config.yaml.example /etc/zfsrabbit/config.yaml
   ```

3. **Edit configuration:**
   ```bash
   sudo nano /etc/zfsrabbit/config.yaml
   ```

4. **Install systemd service:**
   ```bash
   sudo cp zfsrabbit.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable zfsrabbit
   ```

5. **Set admin password:**
   ```bash
   sudo systemctl edit zfsrabbit
   ```
   Add:
   ```
   [Service]
   Environment=ZFSRABBIT_ADMIN_PASSWORD=your-secure-password
   ```

6. **Start the service:**
   ```bash
   sudo systemctl start zfsrabbit
   ```

## Configuration

Edit `/etc/zfsrabbit/config.yaml`:

### Server Settings
```yaml
server:
  port: 8080                           # Web interface port
  admin_pass_env: "ZFSRABBIT_ADMIN_PASSWORD"  # Environment variable for admin password
  log_level: "info"
```

### ZFS Settings
```yaml
zfs:
  dataset: "tank/data"                 # Local dataset to replicate
  send_compression: "lz4"              # ZFS send stream compression (saves bandwidth)
  recursive: true                      # Include child datasets
```

### SSH/Remote Settings
```yaml
ssh:
  remote_host: "backup.example.com"    # Remote backup server
  remote_user: "zfsbackup"             # SSH user
  private_key: "/root/.ssh/id_rsa"     # SSH private key
  remote_dataset: "backup/tank-data"   # Remote dataset
  mbuffer_size: "1G"                   # Buffer size for transfers
```

### Email Alerts
```yaml
email:
  smtp_host: "smtp.gmail.com"
  smtp_port: 587
  smtp_user: "alerts@yourdomain.com"
  smtp_password: "your-app-password"
  from_email: "zfsrabbit@yourdomain.com"
  to_emails:
    - "admin@yourdomain.com"
  use_tls: true
```

### Slack Integration
```yaml
slack:
  webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
  channel: "#zfsrabbit"
  username: "ZFSRabbit"
  icon_emoji: ":rabbit:"
  enabled: true
  alert_on_sync: true     # Send alerts for successful/failed sync operations
  alert_on_errors: true   # Send alerts for system errors
  slash_token: "your-slack-slash-command-token"
```

#### Setting Up Slack Integration

**1. Create a Slack App:**
- Go to https://api.slack.com/apps
- Click "Create New App" → "From scratch"
- Name your app (e.g., "ZFSRabbit") and select your workspace

**2. Set up Incoming Webhooks (for alerts):**
- In your app settings, go to "Incoming Webhooks"
- Toggle "Activate Incoming Webhooks" to On
- Click "Add New Webhook to Workspace"
- Select the channel where you want alerts (e.g., #zfsrabbit)
- Copy the webhook URL (looks like `https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX`)
- Add this URL to your config as `webhook_url`

**3. Create a Slash Command (for interactive commands):**
- In your app settings, go to "Slash Commands"
- Click "Create New Command"
- **Command**: `/zfsrabbit` (or your preferred command name)
- **Request URL**: `http://your-server:8080/slack/command` (replace with your actual server)
- **Short Description**: "ZFS backup system controls"
- **Usage Hint**: `status | snapshot | scrub | jobs | remote | help`
- Click "Save"

**4. Get the Verification Token:**
- In your app settings, go to "Basic Information"
- Under "App Credentials", copy the "Verification Token" 
- Add this token to your config as `slash_token`
- **Note**: Verification tokens are deprecated by Slack. For production use, consider implementing signed secrets for better security

**5. Install the App:**
- In your app settings, go to "Install App"
- Click "Install to Workspace"
- Authorize the app for your workspace

### Scheduling
```yaml
schedule:
  snapshot_cron: "0 2 * * *"           # Daily at 2 AM
  scrub_cron: "0 3 * * 0"              # Weekly Sunday at 3 AM
  monitor_interval: "5m"               # System check interval
```

## Usage

### Web Interface

Access the web interface at `http://your-server:8080`

- Username: `admin`
- Password: Set via `ZFSRABBIT_ADMIN_PASSWORD` environment variable

The web interface provides:
- System status overview
- ZFS pool health monitoring
- Snapshot management
- Manual snapshot creation
- Scrub operations
- **Multi-dataset browsing** - View all datasets on remote server from any ZFSRabbit instance
- **Cross-dataset restore** - Restore from any remote dataset to local system
- **Real-time restore tracking** - Monitor restore job progress with detailed status updates

### Slack Commands

After completing the Slack setup above, you can use these commands:

Available commands:
- `/zfsrabbit status` - Show overall system health
- `/zfsrabbit snapshot` - Create snapshot immediately
- `/zfsrabbit scrub` - Start ZFS pool scrub
- `/zfsrabbit snapshots` - List recent snapshots
- `/zfsrabbit pools` - Show ZFS pool status
- `/zfsrabbit disks` - Show disk health
- `/zfsrabbit restore <snapshot> <dataset>` - Restore a snapshot
- `/zfsrabbit jobs` - Show active restore jobs
- `/zfsrabbit remote` - List all remote datasets
- `/zfsrabbit browse <dataset>` - Browse snapshots in a dataset
- `/zfsrabbit help` - Show help message

### Manual Operations

Create a snapshot immediately:
```bash
curl -X POST -u admin:password http://localhost:8080/api/trigger/snapshot
```

Start a scrub:
```bash
curl -X POST -u admin:password http://localhost:8080/api/trigger/scrub
```

### Logs

View service logs:
```bash
sudo journalctl -u zfsrabbit -f
```

## Security Notes

- Runs as root (required for ZFS operations)
- Uses basic authentication for web interface
- SSH key-based authentication for remote access
- No HTTPS by default (use reverse proxy if needed)

## Monitoring

ZFSRabbit monitors:
- ZFS pool status and errors
- SMART disk health data (traditional HDDs/SATA SSDs)
- **NVMe SSD monitoring** (temperature, wear level, critical warnings, spare capacity)
- Disk temperatures with configurable thresholds
- Reallocated sectors and pending/offline sectors

Email alerts are sent when:
- ZFS pools become degraded
- Disk SMART health checks fail
- High disk temperatures detected (>60°C configurable)
- **NVMe critical warnings** (spare capacity, temperature, reliability issues)
- **NVMe wear level exceeds 90%** (proactive replacement alerts)
- Disk errors found

Slack alerts include:
- ✅ Successful snapshot replication (with duration)
- ❌ Failed snapshot replication (with error details)
- ⚠️ System health issues (pools, disks)
- 📊 System status on demand via slash commands

## Backup Process

1. **Snapshot Creation**: Creates timestamped snapshots of configured dataset
2. **Remote Check**: Lists existing snapshots on remote server
3. **Incremental Detection**: Finds last common snapshot for incremental transfer
4. **Transfer**: Uses `zfs send -c | mbuffer | ssh | zfs receive` pipeline
5. **Cleanup**: Removes old local snapshots (keeps last 30)

## Multi-ZFSRabbit Setup

When multiple ZFSRabbit instances send to the same remote server:

### Remote Dataset Discovery
- **Web Interface**: Shows all remote datasets and their snapshots
- **Slack Commands**: `/zfsrabbit remote` lists all datasets, `/zfsrabbit browse <dataset>` shows details
- **Cross-Restore**: Restore from any remote dataset to local system

### Example Setup
```yaml
# Instance 1 (server-1)
zfs:
  dataset: "tank/web"
ssh:
  remote_dataset: "backup/server-1-web"

# Instance 2 (server-2)  
zfs:
  dataset: "tank/database"
ssh:
  remote_dataset: "backup/server-2-db"
```

Both instances can see and restore from each other's remote datasets.

## Development

### Building from Source
```bash
# Clone repository
git clone https://github.com/yourusername/zfsrabbit.git
cd zfsrabbit

# Run tests (no ZFS required for unit tests)
go test -v ./...

# Build binary
go build -o zfsrabbit .
```

### Testing
ZFSRabbit includes comprehensive unit tests with mock infrastructure, allowing development and testing without requiring actual ZFS pools or SSH servers. All core functionality is covered including HTTP endpoints, alert systems, restore job management, and Slack integration.

## Troubleshooting

### Service won't start
- Check logs: `journalctl -u zfsrabbit`
- Verify configuration file syntax
- Ensure ZFS utilities are available
- Check SSH connectivity to remote server

### Snapshots fail
- Verify dataset exists: `zfs list`
- Check ZFS permissions
- Ensure sufficient disk space

### Remote replication fails
- Test SSH connection manually
- Verify remote dataset exists
- Check mbuffer installation on remote server
- Ensure SSH user has ZFS permissions

### Monitoring alerts not working
- Test email configuration
- Check SMTP credentials
- Verify recipient email addresses
- Check firewall/network connectivity

## License

MIT License - see LICENSE file for details.