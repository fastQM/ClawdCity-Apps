// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

contract AssemblerRegistry {
    error NotOwner();
    error NotContact();
    error InvalidAddress();
    error InvalidBootnode();
    error BootnodeNotFound();
    error ContactAlreadyExists();
    error ContactNotFound();
    error AlreadyPending();
    error RequestNotFound();
    error InvalidRequestStatus();
    error NotApplicant();
    error AlreadyReviewed();
    error NoteTooLong();

    enum RequestStatus {
        None,
        Pending,
        Approved,
        Rejected,
        Cancelled
    }

    struct BootnodeEntry {
        bytes32 id;
        string multiaddr;
        uint64 updatedAt;
    }

    struct ContactRequest {
        uint256 id;
        address applicant;
        string note;
        RequestStatus status;
        uint64 createdAt;
        uint64 reviewedAt;
        address reviewer;
    }

    uint256 public constant MAX_NOTE_LENGTH = 256;

    address public owner;

    // bootnodes
    bytes32[] private _bootnodeIds;
    mapping(bytes32 => BootnodeEntry) private _bootnodes;
    mapping(bytes32 => uint256) private _bootnodePosPlusOne;

    // contacts
    address[] private _contacts;
    mapping(address => bool) public isContact;
    mapping(address => uint256) private _contactPosPlusOne;

    // requests
    uint256 public nextRequestId = 1;
    mapping(uint256 => ContactRequest) private _requests;
    mapping(address => uint256) public pendingRequestIdOf;
    mapping(uint256 => mapping(address => bool)) public reviewedBy;

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    event BootnodeUpserted(bytes32 indexed id, string multiaddr, uint64 updatedAt);
    event BootnodeRemoved(bytes32 indexed id);

    event ContactAdded(address indexed contact, address indexed operator, uint64 at);
    event ContactRemoved(address indexed contact, address indexed operator, uint64 at);

    event ContactRequestSubmitted(uint256 indexed requestId, address indexed applicant, string note, uint64 at);
    event ContactRequestApproved(uint256 indexed requestId, address indexed reviewer, address indexed applicant, uint64 at);
    event ContactRequestRejected(uint256 indexed requestId, address indexed reviewer, address indexed applicant, uint64 at);
    event ContactRequestCancelled(uint256 indexed requestId, address indexed applicant, uint64 at);

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier onlyContact() {
        if (!isContact[msg.sender]) revert NotContact();
        _;
    }

    constructor() {
        owner = msg.sender;
        emit OwnershipTransferred(address(0), msg.sender);
        _addContact(msg.sender, msg.sender);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        address prev = owner;
        owner = newOwner;
        emit OwnershipTransferred(prev, newOwner);
    }

    // --------------------
    // Bootnode management
    // --------------------

    function upsertBootnode(bytes32 id, string calldata multiaddr) external onlyOwner {
        if (id == bytes32(0) || bytes(multiaddr).length == 0) revert InvalidBootnode();

        if (_bootnodePosPlusOne[id] == 0) {
            _bootnodeIds.push(id);
            _bootnodePosPlusOne[id] = _bootnodeIds.length;
        }

        _bootnodes[id] = BootnodeEntry({id: id, multiaddr: multiaddr, updatedAt: uint64(block.timestamp)});
        emit BootnodeUpserted(id, multiaddr, uint64(block.timestamp));
    }

    function removeBootnode(bytes32 id) external onlyOwner {
        uint256 posPlusOne = _bootnodePosPlusOne[id];
        if (posPlusOne == 0) revert BootnodeNotFound();

        uint256 idx = posPlusOne - 1;
        uint256 last = _bootnodeIds.length - 1;
        if (idx != last) {
            bytes32 moved = _bootnodeIds[last];
            _bootnodeIds[idx] = moved;
            _bootnodePosPlusOne[moved] = idx + 1;
        }

        _bootnodeIds.pop();
        delete _bootnodePosPlusOne[id];
        delete _bootnodes[id];

        emit BootnodeRemoved(id);
    }

    function bootnodeCount() external view returns (uint256) {
        return _bootnodeIds.length;
    }

    function getBootnode(bytes32 id) external view returns (BootnodeEntry memory) {
        if (_bootnodePosPlusOne[id] == 0) revert BootnodeNotFound();
        return _bootnodes[id];
    }

    function listBootnodes(uint256 offset, uint256 limit) external view returns (BootnodeEntry[] memory out) {
        if (offset >= _bootnodeIds.length || limit == 0) {
            return new BootnodeEntry[](0);
        }

        uint256 end = offset + limit;
        if (end > _bootnodeIds.length) end = _bootnodeIds.length;

        out = new BootnodeEntry[](end - offset);
        for (uint256 i = offset; i < end; i++) {
            out[i - offset] = _bootnodes[_bootnodeIds[i]];
        }
    }

    // --------------------
    // Contact management
    // --------------------

    function addContact(address who) external onlyOwner {
        _addContact(who, msg.sender);
    }

    function removeContact(address who) external onlyOwner {
        _removeContact(who, msg.sender);
    }

    function contactCount() external view returns (uint256) {
        return _contacts.length;
    }

    function listContacts(uint256 offset, uint256 limit) external view returns (address[] memory out) {
        if (offset >= _contacts.length || limit == 0) {
            return new address[](0);
        }

        uint256 end = offset + limit;
        if (end > _contacts.length) end = _contacts.length;

        out = new address[](end - offset);
        for (uint256 i = offset; i < end; i++) {
            out[i - offset] = _contacts[i];
        }
    }

    // --------------------
    // Contact requests
    // --------------------

    function submitContactRequest(string calldata note) external returns (uint256 requestId) {
        if (bytes(note).length > MAX_NOTE_LENGTH) revert NoteTooLong();
        if (isContact[msg.sender]) revert ContactAlreadyExists();
        if (pendingRequestIdOf[msg.sender] != 0) revert AlreadyPending();

        requestId = nextRequestId++;
        _requests[requestId] = ContactRequest({
            id: requestId,
            applicant: msg.sender,
            note: note,
            status: RequestStatus.Pending,
            createdAt: uint64(block.timestamp),
            reviewedAt: 0,
            reviewer: address(0)
        });
        pendingRequestIdOf[msg.sender] = requestId;

        emit ContactRequestSubmitted(requestId, msg.sender, note, uint64(block.timestamp));
    }

    function approveRequest(uint256 requestId) external onlyOwner {
        ContactRequest storage req = _requests[requestId];
        if (req.id == 0) revert RequestNotFound();
        if (req.status != RequestStatus.Pending) revert InvalidRequestStatus();
        if (reviewedBy[requestId][msg.sender]) revert AlreadyReviewed();

        reviewedBy[requestId][msg.sender] = true;

        req.status = RequestStatus.Approved;
        req.reviewedAt = uint64(block.timestamp);
        req.reviewer = msg.sender;

        if (pendingRequestIdOf[req.applicant] == requestId) {
            pendingRequestIdOf[req.applicant] = 0;
        }

        if (!isContact[req.applicant]) {
            _addContact(req.applicant, msg.sender);
        }

        emit ContactRequestApproved(requestId, msg.sender, req.applicant, uint64(block.timestamp));
    }

    function rejectRequest(uint256 requestId) external onlyOwner {
        ContactRequest storage req = _requests[requestId];
        if (req.id == 0) revert RequestNotFound();
        if (req.status != RequestStatus.Pending) revert InvalidRequestStatus();
        if (reviewedBy[requestId][msg.sender]) revert AlreadyReviewed();

        reviewedBy[requestId][msg.sender] = true;

        req.status = RequestStatus.Rejected;
        req.reviewedAt = uint64(block.timestamp);
        req.reviewer = msg.sender;

        if (pendingRequestIdOf[req.applicant] == requestId) {
            pendingRequestIdOf[req.applicant] = 0;
        }

        emit ContactRequestRejected(requestId, msg.sender, req.applicant, uint64(block.timestamp));
    }

    function cancelMyPendingRequest(uint256 requestId) external {
        ContactRequest storage req = _requests[requestId];
        if (req.id == 0) revert RequestNotFound();
        if (req.applicant != msg.sender) revert NotApplicant();
        if (req.status != RequestStatus.Pending) revert InvalidRequestStatus();

        req.status = RequestStatus.Cancelled;
        req.reviewedAt = uint64(block.timestamp);
        req.reviewer = address(0);

        if (pendingRequestIdOf[msg.sender] == requestId) {
            pendingRequestIdOf[msg.sender] = 0;
        }

        emit ContactRequestCancelled(requestId, msg.sender, uint64(block.timestamp));
    }

    function requestCount() external view returns (uint256) {
        return nextRequestId - 1;
    }

    function getRequest(uint256 requestId) external view returns (ContactRequest memory) {
        ContactRequest memory req = _requests[requestId];
        if (req.id == 0) revert RequestNotFound();
        return req;
    }

    function listRequests(uint256 offset, uint256 limit) external view returns (ContactRequest[] memory out) {
        uint256 total = nextRequestId - 1;
        if (offset >= total || limit == 0) {
            return new ContactRequest[](0);
        }

        uint256 end = offset + limit;
        if (end > total) end = total;

        out = new ContactRequest[](end - offset);
        for (uint256 i = offset; i < end; i++) {
            out[i - offset] = _requests[i + 1];
        }
    }

    // --------------------
    // Internal helpers
    // --------------------

    function _addContact(address who, address operator) internal {
        if (who == address(0)) revert InvalidAddress();
        if (isContact[who]) revert ContactAlreadyExists();

        isContact[who] = true;
        _contacts.push(who);
        _contactPosPlusOne[who] = _contacts.length;

        emit ContactAdded(who, operator, uint64(block.timestamp));
    }

    function _removeContact(address who, address operator) internal {
        uint256 posPlusOne = _contactPosPlusOne[who];
        if (posPlusOne == 0) revert ContactNotFound();

        uint256 idx = posPlusOne - 1;
        uint256 last = _contacts.length - 1;
        if (idx != last) {
            address moved = _contacts[last];
            _contacts[idx] = moved;
            _contactPosPlusOne[moved] = idx + 1;
        }

        _contacts.pop();
        delete _contactPosPlusOne[who];
        isContact[who] = false;

        // allow removed contact to re-apply.
        if (pendingRequestIdOf[who] != 0) {
            pendingRequestIdOf[who] = 0;
        }

        emit ContactRemoved(who, operator, uint64(block.timestamp));
    }
}
