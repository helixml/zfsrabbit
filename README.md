# ZFSRabbit 🐰

A ZFS replication and monitoring server written in Go that provides automated snapshot management, remote replication via SSH/mbuffer, disk health monitoring, and email alerting.

## Features

- **Automated Snapshots**: Configurable cron-based snapshot creation and cleanup
- **Remote Replication**: ZFS send/receive over SSH with mbuffer for efficiency
- **Incremental Backups**: Automatically detects and uses incremental snapshots
- **Disk Monitoring**: SMART data monitoring and ZFS pool health checks
- **Email Alerts**: Configurable email notifications for system issues
- **Slack Integration**: Webhook-based alerts and slash commands for full control
- **Web Interface**: Simple web UI for monitoring and manual operations
- **Restore Functionality**: Web-based restore operations with job tracking
- **Systemd Integration**: Runs as a systemd service with proper logging

## Requirements

- Linux system with ZFS utilities installed
- `smartctl` for disk monitoring
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
- **Multi-dataset browsing** - View all datasets on remote server
- **Cross-dataset restore** - Restore from any remote dataset to local
- Restore job tracking

### Slack Commands

Set up a slash command in Slack (e.g., `/zfsrabbit`) pointing to `http://your-server:8080/slack/command`

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
- SMART disk health data
- Disk temperatures
- Reallocated sectors
- Pending/offline sectors

Email alerts are sent when:
- ZFS pools become degraded
- Disk SMART health checks fail
- High disk temperatures detected
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